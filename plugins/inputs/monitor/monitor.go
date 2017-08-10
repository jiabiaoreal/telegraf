package monitor

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"reflect"
	"sync"
	"time"

	"we.com/jiabiao/common/alert"

	etcddir "we.com/jiabiao/monitor/core/etcd"
	core "we.com/jiabiao/monitor/core/types"
	"we.com/jiabiao/monitor/registry/generic"
	"we.com/jiabiao/monitor/registry/hosts"
	"we.com/jiabiao/monitor/registry/watch"

	"github.com/coreos/etcd/clientv3"
	"github.com/coreos/pkg/capnslog"
	"github.com/golang/glog"
	multierror "github.com/hashicorp/go-multierror"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal/hostinfo"
	"github.com/influxdata/telegraf/plugins/inputs"
	"github.com/influxdata/telegraf/plugins/inputs/monitor/java"
	"github.com/influxdata/telegraf/plugins/inputs/monitor/output"
	psm "github.com/influxdata/telegraf/plugins/inputs/monitor/process"
	"github.com/influxdata/telegraf/plugins/inputs/monitor/types"
)

func getWatchPath() string {
	env := core.ENV(hostinfo.GetEnv())
	return etcddir.GetHostReplicaSpecPath(env, hostinfo.GetHostID())
}

type reportOpts struct {
	MetricInterval         time.Duration
	EtcdKeepalivedInterval time.Duration
	EtcdKeepFlushInterval  time.Duration
}

// HostReplicaSpecManager manager ExpectedReplicaSpec and  ActualReplicaSpec, and make sure the fit each other
// it also watches etcd, for  expected config update
type HostReplicaSpecManager struct {
	ExpectedReplicaSpec *core.HostReplicaSpec
	ActualReplicaSpec   *core.HostReplicaSpec
	LackReplicaSpec     *core.HostReplicaSpec
	ResidualReplicaSpec *core.HostReplicaSpec

	ExpectedUpdateTime time.Time
	ActualUpdateTime   time.Time
	DiffUpdateTime     time.Time

	worker      *psm.StartStopWorker
	eventPusher types.NodeEventPusher

	reportOpts reportOpts
	stopC      chan struct{}

	acc telegraf.Accumulator

	// protect updates of  ExpectedReplicaSpec  and  ActualReplicaSpec
	lock sync.RWMutex

	// proteck updates of LackReplicaSpec and  ResidualReplicaSpec
	diffLock sync.RWMutex
	// time to wait before action take effect
	EffectDuration time.Duration

	// hostRegister  etcdclient
	hostClient *hosts.Registry

	monitorItems map[core.MonitorType]types.Monitor
}

// NewHostReplicaSpecManager  return a new *HostReplicaSpecManager  or error
func NewHostReplicaSpecManager() (*HostReplicaSpecManager, error) {
	var plog = capnslog.NewPackageLogger("github.com/coreos/etcd", "clientv3")
	clientv3.SetLogger(plog)

	// register monitors
	javaMonitor := java.NewStateMonitor()

	ret := &HostReplicaSpecManager{
		reportOpts: reportOpts{
			MetricInterval:         5 * time.Second,
			EtcdKeepalivedInterval: 2 * time.Second,
			EtcdKeepFlushInterval:  20 * time.Second,
		},

		stopC: make(chan struct{}),

		monitorItems: map[core.MonitorType]types.Monitor{
			javaMonitor.GetType(): javaMonitor,
		},
	}
	return ret, nil
}

// UpdateHostInfo update hostinfo to etcd
func (hrsm *HostReplicaSpecManager) UpdateHostInfo() error {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			glog.V(15).Infof("start to update hostinfo")
			hi, err := hrsm.hostClient.GetHostInfoOfHostID(hostinfo.GetHostID())
			if err != nil {
				glog.Errorf("error get hostinfo: %v", err)
				continue
			}

			hi.Annotations = hostinfo.GetAnnotations()

		case <-hrsm.stopC:
			glog.V(10).Info("receiver stop signal, stop update hostinfo")
			return nil
		}

	}
}

// Start implement  telegraf.Input  interface
func (hrsm *HostReplicaSpecManager) Start(acc telegraf.Accumulator) error {
	hrsm.acc = acc

	hrsm.hostClient = hosts.NewRegistry(core.ENV(hostinfo.GetEnv()))

	pusher := NewEventPusher()
	hrsm.eventPusher = pusher

	worker := psm.NewStartStopWorker(acc)
	hrsm.worker = worker

	for _, monitor := range hrsm.monitorItems {
		monitor.InitMonitor(pusher, worker)
	}

	// clean old etcd nodes
	err := hrsm.cleanEtcdData()
	if err != nil {
		glog.Warningf("clean old etcd data err: %v", err)
		return err
	}

	if err = hrsm.ReLoadExpectedSpec(); err != nil {
		return err
	}

	// periodic report and udpate actual ReplicaSpec
	go hrsm.Report()
	// watch expected ReplicaSpec from update
	go hrsm.WatchExpectedSpecUpdate()

	// make sure current instances met configed
	go hrsm.HandleDiffReplicaSpec()

	go hrsm.Probe()

	return nil
}

// Stop implements telegraf.ServiceInput
func (hrsm *HostReplicaSpecManager) Stop() {
	glog.Info("starting to stop process monitor")
	hrsm.worker.Stop()

	glog.Info("send stop signal")
	hrsm.stopC <- struct{}{}

	glog.Info("close stop chan")
	close(hrsm.stopC)

	for _, monitor := range hrsm.monitorItems {
		glog.Info("starting to stop process monitor: %v", monitor.GetType())
		monitor.StopMonitor()
	}

	hrsm.hostClient.Stop()

	time.Sleep(time.Second)
}

// ReLoadExpectedSpec load current HostReplicaSpec from  ectd
func (hrsm *HostReplicaSpecManager) ReLoadExpectedSpec() error {
	hrs, err := hrsm.hostClient.GetReplicaSpec(hostinfo.GetHostID())
	if err != nil {
		se, ok := err.(*generic.StorageError)
		if ok && se.Code == generic.ErrCodeKeyNotFound {
			glog.Info("replica spec not exist, set replica spec to current running instances")
			if err := hrsm.getActualReplicaSpec(); err != nil {
				glog.Errorf("update actual host replica spec err: %v", err)
				return err
			}

			hrs = &core.HostReplicaSpec{}
			hrsm.lock.Lock()
			hrsm.ActualReplicaSpec.Range(func(pt core.ProjectType, tv *core.TypeReplicaSpec) bool {
				tv.Range(func(clustername core.UUID, cv *core.ClusterReplicaSpec) bool {

					hrs.AddCluserSpec(*cv)
					return true
				})
				return true
			})
			hrsm.lock.Unlock()

			// update etcd config
			hrsm.hostClient.SetReplicaSpec(hostinfo.GetHostID(), hrs)
		} else {
			return err
		}
	}

	hrsm.lock.Lock()
	defer hrsm.lock.Unlock()
	hrsm.ExpectedReplicaSpec = hrs
	hrsm.ExpectedUpdateTime = time.Now()
	return nil
}

func (hrsm *HostReplicaSpecManager) handleHostSpecEvent(event watch.Event) error {
	glog.Infof("receve event: %v", event)
	defer func() {
		tags := psm.GetCommonReportTags()
		metric := map[string]interface{}{
			"type": event.Type,
		}

		hrsm.acc.AddFields("replicaWathc", metric, tags)
	}()

	dat, ok := event.Object.(*core.HostReplicaSpec)
	glog.V(10).Infof("typeof event data: %v", reflect.TypeOf(event.Object))
	if !ok {
		alert.SendMsg(fmt.Sprintf("host replica spec watch: unexpected object type: %v ", event))
		glog.Fatalf("event object must be an instance of types.HostIno, got %v", event.Object)
	}

	hostid := hostinfo.GetHostID()
	switch event.Type {
	case watch.Deleted:
		alert.SendMsg(fmt.Sprintf("host replica spec deleted %v, event: %v", hostid, dat))
		glog.Fatalf("host replica spec is deleted: %v", dat)

	case watch.Added, watch.Modified:
		old := hrsm.ExpectedReplicaSpec
		new := dat
		glog.Infof("host replica spec changed from %v to %v", old, new)
		add, remove := core.DiffHostReplicaSpec(old, new)
		glog.Infof("changes: add: %v, remove: %v", add, remove)

		hrsm.ExpectedReplicaSpec = new
		return nil

	case watch.Error:
		err, ok := event.Object.(error)
		if !ok {
			glog.Warningf("event type if error, but event.Object is not an error")
			err = fmt.Errorf("watch got error :%v", event.Object)
		}
		glog.Warningf("watch err: %v", err)

		return nil
	}
	return nil
}

// WatchExpectedSpecUpdate watchs etcd for expected spec update
func (hrsm *HostReplicaSpecManager) WatchExpectedSpecUpdate() error {
	if hrsm == nil || hrsm.hostClient == nil {
		return fmt.Errorf("expectation watch not initialized")
	}

	if hrsm.hostClient == nil {
		log.Fatal("watch host relica spec: etcd client cannot be nil")
	}

	go hrsm.hostClient.WatchHostReplicaSpec(context.TODO(), hostinfo.GetHostID(), hrsm.handleHostSpecEvent)

	return nil
}

func (hrsm *HostReplicaSpecManager) getActualReplicaSpec() error {
	actualSpec := &core.HostReplicaSpec{}
	for typ, monitor := range hrsm.monitorItems {
		ps, err := monitor.GetProcessPidList()
		if err != nil {
			glog.Errorf("error get process of type %v, %v", typ, err)
		}
		for _, p := range ps {
			clustername, version := p.GetClusterNameAndVersion()
			actualSpec.AddCluserSpec(core.ClusterReplicaSpec{
				Type:         core.ProjectType(typ),
				ClusterName:  clustername,
				Version:      version,
				InstancesNum: 1,
			})
		}
	}
	hrsm.lock.Lock()
	hrsm.ActualReplicaSpec = actualSpec
	hrsm.ActualUpdateTime = time.Now()
	hrsm.lock.Unlock()

	return nil
}

// Probe  probes processinfo status
func (hrsm *HostReplicaSpecManager) Probe() {
	timer := time.NewTimer(0)
	for {
		select {
		case <-timer.C:
			glog.V(15).Info("monitor: start to probe")
			wg := sync.WaitGroup{}
			for typ, monitor := range hrsm.monitorItems {
				ps, err := monitor.GetProcessPidList()
				glog.V(10).Infof("end get process list of %v", typ)
				if err != nil {
					glog.Errorf("error get process of type %v, %v", typ, err)
				}

				for _, p := range ps {
					wg.Add(1)
					go func(ps types.ProcessInfor) {
						defer wg.Done()
						ps.Probe()
					}(p)
				}
			}
			wg.Wait()
			glog.V(15).Info("monitor: end probe")
			timer.Reset(10 * time.Second)
		case <-hrsm.stopC:
			return
		}
	}
}

// Report reports process and type spcific metrics to upstream at a certain interval
func (hrsm *HostReplicaSpecManager) Report() {
	metricTimer := time.NewTicker(hrsm.reportOpts.MetricInterval)
	keepaliveTimer := time.NewTicker(hrsm.reportOpts.EtcdKeepalivedInterval)
	flushTimer := time.NewTicker(hrsm.reportOpts.EtcdKeepFlushInterval)

loop:
	for {
		select {
		case <-metricTimer.C:
			s := time.Now()
			tags := psm.GetCommonReportTags()
			for typ, monitor := range hrsm.monitorItems {
				s := time.Now()
				glog.V(10).Infof("start to get process list of %v", typ)
				ps, err := monitor.GetProcessPidList()
				glog.V(10).Infof("end get process list of %v", typ)
				if err != nil {
					glog.Errorf("error get process of type %v, %v", typ, err)
				}
				if err = hrsm.reportMetrics(ps); err != nil {
					glog.Errorf("report metric for type %v: %v", typ, err)
				}
				e := time.Now()
				tags["type"] = string(typ)
				hrsm.acc.AddFields("report_metrics", map[string]interface{}{"took": (e.UnixNano() - s.UnixNano()) / 1e6}, tags)
			}
			e := time.Now()
			tags["type"] = "all"
			hrsm.acc.AddFields("report_metrics", map[string]interface{}{"took": (e.UnixNano() - s.UnixNano()) / 1e6}, tags)

		case <-keepaliveTimer.C:
			hrsm.getActualReplicaSpec()

		case <-flushTimer.C:
			for typ, monitor := range hrsm.monitorItems {
				glog.V(10).Infof("start update instance info for %v", typ)
				if err := monitor.ReportInstances(); err != nil {
					glog.Warningf("update instance of %v err: %v", typ, err)
				}
			}
		case <-hrsm.stopC:
			glog.V(10).Infof("stop report")
			flushTimer.Stop()
			keepaliveTimer.Stop()
			metricTimer.Stop()
			break loop
		}
	}
}

// ReportMetrics  report process metrics to output(influxdb, es etc.)
func (hrsm *HostReplicaSpecManager) reportMetrics(ps []types.ProcessInfor) error {
	var merr *multierror.Error
	for _, p := range ps {
		metric, err := p.GetProcessMetric()
		if err != nil {
			cluster, version := p.GetClusterNameAndVersion()
			glog.Errorf("error get metrics for process %v:%v-%v, %v", cluster, version, p.GetNodeID(), err)
			merr = multierror.Append(merr, err)
			continue
		}

		if metric != nil {
			hrsm.acc.AddFields(metric.Name, metric.Fields, metric.Tags, metric.Time)
		}

		metric, err = p.GetTypeSpecMetrics()
		if err != nil {
			cluster, version := p.GetClusterNameAndVersion()
			glog.Errorf("error get type specific metrics for process %v:%v-%v, %v", cluster, version, p.GetNodeID(), err)
			merr = multierror.Append(merr, err)
			continue
		}
		if metric != nil {
			hrsm.acc.AddFields(metric.Name, metric.Fields, metric.Tags, metric.Time)
		}
	}

	return merr.ErrorOrNil()
}

// wait at mostly  timeout, for actualHostReplicaSpec update, if updated return true, else false
func (hrsm *HostReplicaSpecManager) waitActualSpecUpdateWithTimeout(timeout time.Duration) bool {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	start := time.Now()

	for {
		select {
		case e := <-ticker.C:
			// if actualSpecs updated  1 minute ago
			if hrsm.ActualUpdateTime.Add(time.Minute).Before(e) {
				// if timeout
				if start.Add(timeout).Before(e) {
					return false
				}
				continue
			}

			// ActualSpecs updated within 1 minute, we also need to make sure, updatetime after  diff update time
			// first time
			if hrsm.DiffUpdateTime.IsZero() {
				return true
			}

			if hrsm.DiffUpdateTime.After(hrsm.ActualUpdateTime) {
				// if timeout
				if start.Add(timeout).Before(e) {
					return false
				}
				continue
			}

			hrsm.lock.Lock()
			lack, residual := core.DiffHostReplicaSpec(hrsm.ActualReplicaSpec, hrsm.ExpectedReplicaSpec)
			hrsm.lock.Unlock()

			// note: make sure unlock get called
			hrsm.diffLock.Lock()
			d1, d2 := core.DiffHostReplicaSpec(hrsm.LackReplicaSpec, lack)

			if d1 == nil && d2 == nil {
				d1, d2 = core.DiffHostReplicaSpec(hrsm.ResidualReplicaSpec, residual)
			}
			hrsm.diffLock.Unlock()

			if d1 != nil || d2 != nil {
				return true
			}

			if hrsm.LackReplicaSpec == nil && hrsm.ResidualReplicaSpec == nil {
				return true
			}

			// timeout
			if start.Add(timeout).Before(e) {
				return false
			}
		}
	}
}

// HandleDiffReplicaSpec  make sure actual HostReplicaSpec meet expected hostReplicaSpec
func (hrsm *HostReplicaSpecManager) HandleDiffReplicaSpec() error {
	timer := time.NewTicker(2 * time.Second)
	defer timer.Stop()

	logoutput := output.NewLogOutputer(0)
	waitcount := 0
	nextActionStart := false

	for {
		select {
		case <-timer.C:
			// note: if both lack and residual is not empty, we should first stop residual instance one by one
			// after that, we start lack instances one by one, that can save host resources, or
			// we may cannot start new instances for lack of memory, threads, etc.
			// maybe it better to stop one followed by start one ...

			// wait autualSpec and expectedSpec for update  and stops wait if EffectDuration time have elapsed
			if !hrsm.waitActualSpecUpdateWithTimeout(5 * time.Second) {
				waitcount++
				if (waitcount % 12) == 0 {
					glog.Warningf("waited more than 1 minutes, project cannnot stop process")
				} else {
					continue
				}
			}

			// here we make sure both actual  and expectation  are up to date
			hrsm.lock.Lock()
			lack, residual := core.DiffHostReplicaSpec(hrsm.ActualReplicaSpec, hrsm.ExpectedReplicaSpec)
			hrsm.LackReplicaSpec = lack
			hrsm.ResidualReplicaSpec = residual
			hrsm.DiffUpdateTime = time.Now()
			hrsm.lock.Unlock()

			glog.V(4).Infof("diff lack: %v\nresidual: %v", lack, residual)

			if lack == nil && residual == nil {
				glog.V(10).Infof("HostReplica Handler expectation meet: %v", hrsm.ExpectedReplicaSpec)
				nextActionStart = false
				continue
			} else {
				glog.V(10).Infof("host acutal replica spec：%v\nexpect: %v", hrsm.ActualReplicaSpec, hrsm.ExpectedReplicaSpec)
			}

			// stop has a high priority
			if residual != nil && !nextActionStart {
				// find which instances to stop
				a := hrsm.findBestInstanceToStop(residual)
				if a == nil {
					glog.Errorf("cannot find which process to stop: %v", residual)
				} else {
					cluster, version := a.GetClusterNameAndVersion()
					glog.Infof("start to stop process: %v %v %v", a.GetMonitorType(), cluster, version)
					typ := a.GetMonitorType()
					monitor := hrsm.monitorItems[typ]
					monitor.StopProcess(a, logoutput)
				}
				if lack != nil {
					nextActionStart = true
				}
				continue
			}

			// start new  insances
			if lack != nil {
				// we has not opinion  which one should start first, random pick one
				chooseFrom := []*core.ClusterReplicaSpec{}
				lack.Range(func(pt core.ProjectType, typSpec *core.TypeReplicaSpec) bool {
					typSpec.Range(func(cluster core.UUID, cspec *core.ClusterReplicaSpec) bool {
						if cspec.InstancesNum > 0 {
							spec := &core.ClusterReplicaSpec{
								Type:         pt,
								ClusterName:  cluster,
								Version:      cspec.Version,
								InstancesNum: 1,
							}

							chooseFrom = append(chooseFrom, spec)
						}
						return true
					})
					return true
				})

				if len(chooseFrom) > 0 {
					idx := rand.Intn(len(chooseFrom))

					spec := chooseFrom[idx]
					typ := core.MonitorType(spec.Type)
					monitor := hrsm.monitorItems[typ]
					glog.Infof("start new %v instance %v", spec.Type, spec.ClusterName)
					if err := monitor.StartProcess(spec, logoutput); err != nil {
						glog.Errorf("error start new %v instance %v %v", spec.Type, spec.ClusterName, err)
					}
					glog.Warningf("ret: %v", logoutput.GetDataString())
					logoutput.Reset()
				}
			}

			nextActionStart = false

		case <-hrsm.stopC:
			glog.V(10).Infof("stop handle host replica diff")
			return nil
		}

	}
}

type cmd string

const (
	cmdStart   cmd = "start"
	cmdStop    cmd = "stop"
	cmdRestart cmd = "restart"
	cmdNoop    cmd = "noop"
)

type action struct {
	cmd     cmd
	typ     core.MonitorType
	cluster string
	node    string
}

// findInstanceToStop
func (hrsm *HostReplicaSpecManager) findBestInstanceToStop(residual *core.HostReplicaSpec) types.ProcessInfor {
	if residual == nil {
		return nil
	}

	hrsm.diffLock.Lock()
	defer hrsm.diffLock.Unlock()

	match := []types.ProcessInfor{}

	residual.Range(func(pt core.ProjectType, typSpec *core.TypeReplicaSpec) bool {
		typ := core.MonitorType(pt)
		monitor := hrsm.monitorItems[typ]
		ps, err := monitor.GetProcessPidList()
		if err != nil {
			glog.Warningf("get process list of type %v err: %v", typ, err)
		}
		for _, p := range ps {
			cluster, version := p.GetClusterNameAndVersion()
			if v := typSpec.Get(cluster, version); v != nil {
				match = append(match, p)
			}
		}
		return true
	})

	var bestMatch types.ProcessInfor
	largest := .0
	for _, p := range match {
		if p.GetResConsumerEvaluate() >= largest {
			bestMatch = p
		}
	}

	return bestMatch
}

func (hrsm *HostReplicaSpecManager) cleanEtcdData() error {
	for typ, monitor := range hrsm.monitorItems {
		if err := monitor.CleanOldInstances(); err != nil {
			glog.Errorf("clean onld instance of type %v err: %v", typ, err)
		}
	}
	return nil
}

// Description implements telegraf.ServiceInput
func (hrsm *HostReplicaSpecManager) Description() string {
	return "monitor configed  prcess and period report process metrics"
}

// SampleConfig implements telegraf.ServiceInput
func (hrsm *HostReplicaSpecManager) SampleConfig() string {
	return ""
}

// Gather implements telegraf.ServiceInput
func (hrsm *HostReplicaSpecManager) Gather(acc telegraf.Accumulator) error {
	return nil
}

func init() {
	inputs.Add("monitor", func() telegraf.Input {
		ret, err := NewHostReplicaSpecManager()
		if err != nil {
			log.Fatalf("create process Monitor %v", err)
		}
		return ret
	})
}

var _ telegraf.ServiceInput = &HostReplicaSpecManager{}
