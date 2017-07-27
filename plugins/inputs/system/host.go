// +build !windows

package system

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/influxdata/telegraf"
	hinf "github.com/influxdata/telegraf/internal/hostinfo"
	"github.com/influxdata/telegraf/plugins/inputs"

	"we.com/jiabiao/common/runtime"
	"we.com/jiabiao/monitor/core/types"
	"we.com/jiabiao/monitor/registry/hosts"

	"github.com/golang/glog"
	multierror "github.com/hashicorp/go-multierror"
	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/disk"
	"github.com/shirou/gopsutil/load"
	"github.com/shirou/gopsutil/mem"
	"github.com/shirou/gopsutil/net"
)

type hostInfo struct {
	lastStat *types.HostStatus
	newStat  *types.HostStatus
	update   time.Time
	DiskIO   map[string]disk.IOCountersStat

	netUpdateTime time.Time
	NetIOStat     map[string]net.IOCountersStat

	store *hosts.Registry
	info  types.HostInfo

	lock sync.RWMutex

	reportC chan struct{}
	stopC   chan struct{}
}

func (hi *hostInfo) SampleConfig() string {
	return ""
}

func (hi *hostInfo) Description() string {
	return ""
}

func (hi *hostInfo) Gather(acc telegraf.Accumulator) error {
	loadStat, err := load.Avg()
	if err != nil {
		glog.Errorf("get load error: %v", err)
	}

	percent, _ := cpu.Percent(0, false)

	procs, threads := numOfProcessAndThreads()

	hs := types.HostStatus{
		HostName:       hinf.GetHostName(),
		IP:             hinf.GetInternalIP(),
		UpdateTime:     time.Now(),
		HostID:         hinf.GetHostID(),
		Load1:          loadStat.Load1,
		Load5:          loadStat.Load5,
		Load15:         loadStat.Load15,
		CPUUsage:       percent[0],
		NumofProcesses: procs,
		NumOfThreads:   threads,
	}

	swap, err := mem.SwapMemory()
	if err != nil {
		glog.Errorf("get swap mem error: %v", err)
	} else {
		hs.FreeSwap = swap.Free
		hs.Sin = swap.Sin
		hs.Sout = swap.Sout
	}

	vm, err := mem.VirtualMemory()
	if err != nil {
		glog.Errorf("get virtual mem: %v", err)
	} else {
		hs.FreeMemory = vm.Free
		hs.CachedMemory = vm.Cached
	}

	hi.newStat = &hs

	hi.netIO()
	hi.diskIO()

	if _, err = hi.store.UpdateResource(&hs); err != nil {
		glog.Errorf("update host status: %v", err)
	}

	return nil
}

func numOfThreads(pid int32) int {
	statPath := fmt.Sprintf("/proc/%v/status", pid)

	file, err := os.Open(statPath)
	if err != nil {
		glog.V(10).Infof("get num of threads of process %v, err: %v", pid, err)
		return 0
	}

	reader := bufio.NewReader(file)

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		if !strings.HasPrefix(line, "Threads:") {
			continue
		}
		tabParts := strings.SplitN(line, "\t", 2)
		if len(tabParts) < 2 {
			break
		}
		value := tabParts[1]
		switch strings.TrimRight(tabParts[0], ":") {
		case "Threads":
			v, err := strconv.ParseInt(value, 10, 32)
			if err != nil {
				glog.Warningf("parse threads fields of '%v', err: %v", line, err)
				return 0
			}
			return int(v)
		}
	}
	return 0
}

func numOfProcessAndThreads() (int, int) {
	var numP int64
	var numT int64
	var fields map[string]interface{}
	if cachedData != nil {
		fields = cachedData
	} else {
		fields = getEmptyFields()
		gatherFromProcWalk(fields, readProcFile)
	}
	p, ok := fields["total"]
	if ok {
		numP, _ = p.(int64)
	}
	t, ok := fields["total_threads"]
	if ok {
		numT, _ = t.(int64)
	}

	return int(numP), int(numT)
}

func (hi *hostInfo) diskIO() error {

	disks := hinf.GetDiskStat()

	if len(disks) == 0 {
		return nil
	}

	now := time.Now()
	names := []string{}
	for k := range disks {
		names = append(names, k)
	}
	diskio, err := disk.IOCounters(names...)
	if err != nil {
		err := fmt.Errorf("get disk partiton stat: %v", err)
		glog.Errorf("%v", err)
		return err
	}

	// first time
	if len(hi.DiskIO) == 0 {
		hi.DiskIO = diskio
		return nil
	}

	dur := uint64(now.Unix() - hi.update.Unix())
	if dur == 0 {
		dur = 1
	}

	var merr *multierror.Error

	diskStats := map[string]types.DiskStat{}
	for k, io := range diskio {
		old, ok := hi.DiskIO[k]
		if !ok {
			glog.Warningf("old value not found for: %v", k)
			continue
		}

		key := disks[k].Mountpoint
		diskStat := types.DiskStat{
			Devices:    k,
			ReadSpeed:  (io.ReadBytes - old.ReadBytes) / dur,
			WriteSpeed: (io.WriteBytes - old.WriteBytes) / dur,
			ReadCount:  (io.ReadCount - old.ReadCount) / dur,
			WriteCount: (io.WriteCount - old.WriteCount) / dur,
		}

		usage, err := disk.Usage(key)
		if err != nil {
			merr = multierror.Append(merr, err)
			diskStats[key] = diskStat
			continue
		}

		diskStat.Totoal = usage.Total
		diskStat.Free = usage.Free
		diskStats[key] = diskStat
	}

	if hi.newStat == nil {
		hi.newStat = &types.HostStatus{}
	}

	hi.newStat.DiskStat = diskStats
	hi.update = now

	return merr.ErrorOrNil()
}

func (hi *hostInfo) netIO() error {
	iocs, err := net.IOCounters(false)
	if err != nil {
		glog.V(10).Infof("get net io stat: %v", err)
		return err
	}

	now := time.Now()
	if len(hi.NetIOStat) == 0 {
		stat := map[string]net.IOCountersStat{}
		for _, n := range iocs {
			stat[n.Name] = n
		}
		hi.NetIOStat = stat
		hi.netUpdateTime = now
		return nil
	}
	dur := uint64(now.Unix() - hi.netUpdateTime.Unix())
	if dur == 0 {
		dur = 1
	}

	netStats := map[string]types.NetIOStat{}
	newIoStats := map[string]net.IOCountersStat{}
	for _, n := range iocs {
		newIoStats[n.Name] = n
		old, ok := hi.NetIOStat[n.Name]
		if !ok {
			glog.Infof("cannot get net iostat for: %v", n.Name)
			continue
		}

		stat := types.NetIOStat{
			Name:        n.Name,
			BytesRecv:   (n.BytesRecv - old.BytesRecv) / dur,
			BytesSent:   (n.BytesSent - old.BytesSent) / dur,
			PacketsRecv: (n.PacketsRecv - old.PacketsRecv) / dur,
			PacketsSent: (n.PacketsSent - old.PacketsSent) / dur,
		}

		netStats[n.Name] = stat
	}

	if hi.newStat == nil {
		hi.newStat = &types.HostStatus{}
	}

	hi.newStat.BandWidthUsage = netStats

	hi.NetIOStat = newIoStats
	hi.netUpdateTime = now
	return nil
}

func (hi *hostInfo) reportHostInfo() error {
	defer runtime.HandleCrash()
	info := types.HostInfo{
		HostID:     hinf.GetHostID(),
		HostName:   hinf.GetHostName(),
		ENV:        hinf.GetEnv(),
		Disk:       hinf.GetDiskStat(),
		NumOfCPUs:  hinf.GetNumOfCPUs(),
		Memory:     hinf.GetMemory(),
		SwapSize:   hinf.GetSwapSize(),
		IPs:        hinf.GetIPs(),
		UpdateTime: time.Now(),
		Labels:     hinf.GetLabels(),
	}

	if hi.info.ENV != info.ENV {
		// if env has changed(this may be happen, when we change host env config), we should create a new store
		oldStore := hi.store
		hi.lock.Lock()
		hi.store = hosts.NewRegistry(info.ENV)
		hi.lock.Unlock()

		//clean  previous report hostinfo
		if err := oldStore.DelHostInfo(info.HostID); err != nil {
			glog.Errorf("delete old hostinfo: %v", err)
		}
	}

	hi.lock.Lock()
	hi.info = info
	hi.lock.Unlock()

	return hi.store.SaveHostInfo(&info)
}

// Start starts the ServiceInput's service, whatever that may be
func (hi *hostInfo) Start(telegraf.Accumulator) error {
	go func() {
		timer := time.NewTimer(0)
		for {
			select {
			case <-hi.reportC:
				if err := hi.reportHostInfo(); err != nil {
					glog.Errorf("report hostinfo: %v", err)
				}
				timer.Reset(5 * time.Minute)
			case <-timer.C:
				if err := hi.reportHostInfo(); err != nil {
					glog.Errorf("report hostinfo: %v", err)
				}
				timer.Reset(5 * time.Minute)
			case <-hi.stopC:
				return
			}

		}
	}()
	return nil
}

// Stop stops the services and closes any necessary channels and connections
func (hi *hostInfo) Stop() {
}

func init() {
	inputs.Add("hostStat", func() telegraf.Input {
		return &hostInfo{
			store:   hosts.NewRegistry(hinf.GetEnv()),
			reportC: make(chan struct{}),
			stopC:   make(chan struct{}),
			info: types.HostInfo{
				HostID:    hinf.GetHostID(),
				HostName:  hinf.GetHostName(),
				ENV:       hinf.GetEnv(),
				Disk:      hinf.GetDiskStat(),
				NumOfCPUs: hinf.GetNumOfCPUs(),
				Memory:    hinf.GetMemory(),
				SwapSize:  hinf.GetSwapSize(),
				IPs:       hinf.GetIPs(),
			},
		}
	})
}
