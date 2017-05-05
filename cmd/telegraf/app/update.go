package app

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/golang/glog"
	update "github.com/inconshreveable/go-update"

	"encoding/hex"

	psutil "github.com/shirou/gopsutil/process"

	"github.com/influxdata/telegraf/internal/hostinfo"
	"github.com/influxdata/telegraf/plugins/inputs/monitor/process"
	"we.com/jiabiao/common/runtime"
	"we.com/jiabiao/monitor/core/types"
	"we.com/jiabiao/monitor/registry/generic"
	"we.com/jiabiao/monitor/registry/watch"
)

const (
	dateFormat       = "2006-01-02 15:04"
	statusPathPrefix = "status"
	watchPathPrefix  = "update"
	configPathPrefix = "update"
)

func init() {
	if env == "" {
		env = string(hostinfo.GetEnv())
	}
	reportPrefix = filepath.Join("/mnt/agent", env)
}

var (
	env          string
	reportPrefix string
)

// everytime env change, we should first stop update watch and config watch,
// and then restart watch with a different path, and we should alse write env info to disk
// so that can survive watchdog restart
func (u *updater) updateEnv(newEnv types.ENV) {
	u.lock.Lock()
	defer u.lock.Unlock()

	hostinfo.UpdateEnv(newEnv)
	close(u.stopC)

	// wait updater stopped
	glog.Infof("start wait updater to stop")
	u.wg.Wait()
	glog.Infof("end wait updater to stop")

	up := &updater{
		hostID:           hostinfo.GetHostID(),
		absPath:          u.absPath,
		watdogVersion:    u.watdogVersion,
		agentVersion:     u.agentVersion,
		lastUpdateTime:   u.lastUpdateTime,
		lastUpdateStatus: u.lastUpdateStatus,
		successCount:     u.successCount,
		updateCount:      u.updateCount,
		lastHostConfig:   u.lastHostConfig,
		stopC:            make(chan struct{}),
		reportC:          make(chan struct{}, 2),
	}
	// create a new updater

	go up.Start()
}

// Date date time
type Date time.Time

// UnmarshalJSON implements json.Unmarshaler interface
func (d *Date) UnmarshalJSON(data []byte) error {
	dstr := string(data)
	if dstr == "null" {
		return nil
	}

	t, err := time.Parse(`"`+dateFormat+`"`, dstr)
	if err != nil {
		return err
	}

	*d = Date(t)
	return nil
}

// MarshalJSON implements json.Marshaler interface
func (d Date) MarshalJSON() ([]byte, error) {
	t := time.Time(d)
	b := make([]byte, 0, len(dateFormat)+2)
	b = append(b, '"')
	b = t.AppendFormat(b, dateFormat)
	b = append(b, '"')
	return b, nil
}

type agentInfo struct {
	WatchdogVersion  string                 `json:"watchdog_version,omitempty"`
	HostName         string                 `json:"hostName,omitempty"`
	IPS              map[string]string      `json:"ips,omitempty"`
	HostID           string                 `json:"hostID,omitempty"`
	ENV              string                 `json:"env,omitempty"`
	AgentVersion     string                 `json:"agentVersion,omitempty"`
	LastStartTime    Date                   `json:"lastStartTime,omitempty"`
	LastUpdateStatus string                 `json:"lastUpdateStatus,omitempty"`
	RestartTimes     int                    `json:"restartTimes,omitempty"`
	UpdateCount      int                    `json:"updateCount,omitempty"`
	SuccessCount     int                    `json:"successCount,omitempty"`
	ResUsage         types.InstanceResUsage `json:"resUsage,omitempty"`
	ReportTime       Date                   `json:"reportTime,omitempty"`
}

type updateConfig struct {
	Version      string `json:"version,omitempty"`
	SHA256       string `json:"sha256,omitempty"`
	UpdateURL    string `json:"updateURL,omitempty"`
	RestartWatch bool   `json:"restartWatch"`
}

// updater update binary file
type updater struct {
	lock             sync.Mutex
	absPath          string
	watdogVersion    string
	agentVersion     string
	lastUpdateTime   time.Time
	lastUpdateStatus string
	updateCount      int
	successCount     int
	hostID           types.UUID
	stopC            chan struct{}
	reportC          chan struct{}
	wg               sync.WaitGroup
	lastHostConfig   *types.HostConfig
}

// Updater update the running binary to the specific version
type Updater interface {
	Start()
	Stop()
}

// NewUpdater returns a new Updater
func NewUpdater() (Updater, error) {
	ap, err := os.Executable()
	if err != nil {
		return nil, err
	}

	wv, err := getDownloadedVersion(ap)
	if err != nil {
		return nil, err
	}
	u := &updater{
		hostID:         hostinfo.GetHostID(),
		stopC:          make(chan struct{}),
		reportC:        make(chan struct{}, 2),
		absPath:        ap,
		watdogVersion:  wv,
		agentVersion:   wv,
		lastUpdateTime: time.Now(),
	}
	return u, nil
}

type watchFunc func() error

func (u *updater) watchloop(wf watchFunc) error {
	u.wg.Add(1)
	defer u.wg.Done()
	for {
		select {
		default:
			if err := wf(); err != nil {
				glog.Errorf("watch err: %v", err)
			}
			time.Sleep(10 * time.Second)
		case <-u.stopC:
			glog.Infof("receiver stop signal, stop")
			return nil
		}
	}
}

func (u *updater) Start() {
	go u.reportStat()
	go u.watchloop(u.watch)
	go u.watchloop(u.watchHostConfig)
}

func (u *updater) Stop() {
	close(u.stopC)
}

func (u *updater) watch() error {
	defer runtime.HandleCrash()
	watchPath := filepath.Join(watchPathPrefix, string(u.hostID))

	store, err := generic.GetStoreInstance(reportPrefix, false)
	if err != nil {
		return err
	}
	w, err := store.Watch(context.Background(), watchPath, generic.Everything, false, reflect.TypeOf(updateConfig{}))
	if err != nil {
		return err
	}

	defer w.Stop()
	for {
		select {
		case event := <-w.ResultChan():
			glog.V(10).Infof("receive an instance event: %v", event)
			switch event.Type {
			case watch.Error:
				err, ok := event.Object.(error)
				if !ok {
					glog.Warningf("event type if error, but event.Object is not an error")
					err = fmt.Errorf("watch got error :%v", event.Object)
				}
				glog.Warningf("watch err: %v", err)
			default:
				if err := u.handlerEvent(event); err != nil {
					glog.Fatalf("handle instance event: %v", err)
				}
			}
		case <-u.stopC:
			return nil
		}
	}
}

func downloadfile(out io.Writer, url string) error {
	// Create the file
	// Get the data
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	r := resp.Body
	var reader io.Reader = r
	if strings.HasSuffix(url, ".gz") {
		reader, err = gzip.NewReader(r)
		if err != nil {
			return err
		}
	}

	// Writer the body to file
	_, err = io.Copy(out, reader)
	if err != nil {
		return err
	}

	return nil
}

func (u *updater) handlerEvent(event watch.Event) error {
	uc, ok := event.Object.(*updateConfig)
	if !ok {
		glog.Warningf("unknown event: %v", reflect.TypeOf(event.Object))
	}

	glog.Infof("receiver event: %v, %v", event.Type, uc)

	switch event.Type {
	case watch.Added, watch.Modified:
		if uc.Version == u.agentVersion {
			glog.Infof("already running version: %v", u.agentVersion)
			return nil
		}

		go func(cfg updateConfig) {
			glog.Infof("start to update to version: %v", cfg.Version)
			u.lock.Lock()
			oldversion := u.agentVersion
			u.lock.Unlock()
			for {
				// random sleep [0,10] seconds
				time.Sleep(time.Duration(rand.Int31n(10)) * time.Second)
				u.lock.Lock()
				if oldversion != u.agentVersion {
					u.lock.Unlock()
					break
				}
				u.lock.Unlock()
				if err := u.updateToVersion(cfg); err != nil {
					glog.Errorf("udate agent: %v", err)
					time.Sleep(65 * time.Minute)
				} else {
					break
				}
			}

			glog.Info("end update agent")
		}(*uc)

	case watch.Deleted:
		glog.Warningf("update config deleted")
	}

	return nil
}

func (u *updater) updateToVersion(cfg updateConfig) error {
	glog.V(10).Infof("update to version: %v, %v", cfg.Version, cfg.SHA256)
	defer func() { u.reportC <- struct{}{} }()
	u.lock.Lock()
	defer u.lock.Unlock()
	u.updateCount++

	tmpfile, err := ioutil.TempFile("", "example")
	if err != nil {
		glog.Fatal(err)
	}

	defer os.Remove(tmpfile.Name()) // clean up
	defer tmpfile.Close()

	glog.V(10).Infof("start to download from: %v", cfg.UpdateURL)
	if err = downloadfile(tmpfile, cfg.UpdateURL); err != nil {
		u.lastUpdateStatus = fmt.Sprintf("down file failed: %v", err)
		return err
	}

	tmpfile.Seek(0, os.SEEK_SET)

	version, err := hex.DecodeString(cfg.SHA256)
	if err != nil {
		glog.Errorf("not a valid hex string: %v, %v", cfg.SHA256, err)
		u.lastUpdateStatus = fmt.Sprintf("sha256 check err: %v", err)
		return err
	}

	opts := update.Options{
		TargetPath:  u.absPath,
		Checksum:    version,
		OldSavePath: fmt.Sprintf("%v.orig", u.absPath),
	}
	glog.V(10).Info("start to apply update")
	if err = update.Apply(tmpfile, opts); err != nil {

		u.lastUpdateStatus = fmt.Sprintf("apply binary file error: %v", err)
		return err
	}

	u.agentVersion = cfg.Version
	glog.V(10).Info("start to restart child")
	Restart(!cfg.RestartWatch)
	u.successCount++
	u.lastUpdateTime = time.Now()
	u.lastUpdateStatus = "success"
	return nil
}

// check actual version info of download file
func getDownloadedVersion(bin string) (string, error) {
	info, err := os.Stat(bin)
	if err != nil {
		return "", err
	}

	if info.Mode()|(0500) == 0 {
		oldMode := info.Mode() | os.ModePerm
		os.Chmod(bin, 0777)
		defer os.Chmod(bin, oldMode)
	}

	cmd := exec.Command(bin, "--version")
	var out bytes.Buffer
	cmd.Stdout = &out
	err = cmd.Run()

	return strings.TrimSpace(out.String()), err
}

func (u *updater) reportStat() error {
	pid := os.Getpid()
	procs, err := psutil.NewProcess(int32(pid))
	if err != nil {
		glog.Errorf("create psutils process error: %v", err)
		return nil
	}

	timer := time.NewTimer(0)
	u.wg.Add(1)
	defer u.wg.Done()
	for {
		select {
		case <-timer.C:
			glog.V(5).Info("start to report watch status")
			rc := 0
			if wd != nil {
				rc = int(wd.startCount)
			}

			ps := process.CalProcessState(procs)
			ai := agentInfo{
				ReportTime:       Date(time.Now()),
				HostName:         hostinfo.GetHostName(),
				HostID:           string(hostinfo.GetHostID()),
				ENV:              env,
				IPS:              hostinfo.GetIPs(),
				WatchdogVersion:  u.watdogVersion,
				AgentVersion:     u.agentVersion,
				LastUpdateStatus: u.lastUpdateStatus,
				LastStartTime:    Date(u.lastUpdateTime),
				RestartTimes:     rc,
				UpdateCount:      u.updateCount,
				SuccessCount:     u.successCount,
				ResUsage: types.InstanceResUsage{
					Memory:         ps.MemInfo.RSS,
					CPUTotal:       ps.CPUInfo.Total(),
					CPUPercent:     ps.CPUPercent,
					Threads:        ps.NumThreads,
					DiskBytesRead:  ps.DiskIO.ReadBytes,
					DiskBytesWrite: ps.DiskIO.WriteBytes,
				},
			}

			reportPath := filepath.Join(statusPathPrefix, string(u.hostID))

			store, err := generic.GetStoreInstance(reportPrefix, false)
			if err != nil {
				timer.Reset(15 * time.Second)
				glog.Warningf("report watch status err: %v", err)
				continue
			}
			if err = store.Update(context.TODO(), reportPath, &ai, nil, 0); err != nil {
				glog.Warningf("report watch status err: %v", err)
			}

			timer.Reset(5 * time.Minute)
		case <-u.reportC:
			timer.Reset(0)
		case <-u.stopC:
			glog.Infof("stop watch info report")
			return nil
		}

	}
}

func (u *updater) watchHostConfig() error {
	defer runtime.HandleCrash()
	watchPath := filepath.Join(configPathPrefix, string(u.hostID))

	store, err := generic.GetStoreInstance(reportPrefix, false)
	if err != nil {
		return err
	}
	w, err := store.Watch(context.Background(), watchPath, generic.Everything, false, reflect.TypeOf(types.HostConfig{}))
	if err != nil {
		return err
	}

	defer w.Stop()
	for {
		select {
		case event := <-w.ResultChan():
			glog.V(10).Infof("receive an instance event: %v", event)
			switch event.Type {
			case watch.Error:
				err, ok := event.Object.(error)
				if !ok {
					glog.Warningf("event type if error, but event.Object is not an error")
					err = fmt.Errorf("watch got error :%v", event.Object)
				}
				glog.Warningf("watch err: %v", err)
			default:
				if err := u.handlerHostConfigChange(event); err != nil {
					glog.Fatalf("handle instance event: %v", err)
				}
			}
		case <-u.stopC:
			return nil
		}
	}
}

func (u *updater) handlerHostConfigChange(event watch.Event) error {
	hc, ok := event.Object.(*types.HostConfig)
	if !ok {
		glog.Warningf("unknown event: %v", reflect.TypeOf(event.Object))
	}

	glog.Infof("receiver host config change event: %v, %v", event.Type, hc)

	switch event.Type {
	case watch.Added, watch.Modified:

	case watch.Deleted:
		glog.Warningf("host config config deleted")
	}

	return nil
}

func (u *updater) applyHostConfig(hc *types.HostConfig) error {
	// things we care about:  reportTags,
	// inputlist, outputList, env

	// right now, we only care about env only
	if hc.ENV != hostinfo.GetEnv() {
		go u.updateEnv(hc.ENV)
	}

	u.lastHostConfig = hc
	return nil
}
