package java

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/golang/glog"
	multierror "github.com/hashicorp/go-multierror"
	"github.com/influxdata/telegraf/internal/config"
	"github.com/influxdata/telegraf/internal/hostinfo"
	psm "github.com/influxdata/telegraf/plugins/inputs/monitor/process"
	"github.com/influxdata/telegraf/plugins/inputs/monitor/types"
	"github.com/pkg/errors"
	"github.com/shirou/gopsutil/process"

	"we.com/jiabiao/common/probe"
	pjava "we.com/jiabiao/common/probe/java"
	"we.com/jiabiao/monitor/core/java"
	core "we.com/jiabiao/monitor/core/types"
	"we.com/jiabiao/monitor/registry/clusters"
	"we.com/jiabiao/monitor/registry/instances"
	"we.com/jiabiao/monitor/registry/watch"
)

const (
	monitorType = java.MonitorType
	projectType = java.Type
)

// ProcessInfos implements types.ProcessInfor interface
type ProcessInfos struct {
	jins             *core.Instance
	proc             *process.Process
	state            *psm.ProcessState
	probStateChanged bool
	updateTime       time.Time
}

// StateMonitor java process monitor
type StateMonitor struct {
	reportIP string

	closeC chan struct{}

	eventPusher types.NodeEventPusher
	worker      types.ProcessWorker
	lock        sync.Mutex               // project update and iter  psState and pidMap
	pidMap      map[int]*ProcessInfos    // key: pid
	psState     map[string]*ProcessInfos // processInfo map, key is clusterID

	store *instances.Registry
}

// NewStateMonitor reset a new state Monitor
func NewStateMonitor() *StateMonitor {
	reportIP := os.Getenv("JAVA_SERVER_REPORT_HOST")

	// if reportIP is empty or there is an error, try to set reportIP to internal ip
	if reportIP == "" {
		reportIP = hostinfo.GetInternalIP()
		if reportIP == "" {
			ips := hostinfo.GetExternalIPS()
			if len(ips) > 0 {
				reportIP = ips[0]
			} else {
				reportIP = "localhost"
			}
		}
	}

	return &StateMonitor{
		reportIP: reportIP,
		closeC:   make(chan struct{}),
		pidMap:   map[int]*ProcessInfos{},
		psState:  map[string]*ProcessInfos{},

		store: instances.NewRegistry(hostinfo.GetEnv()),
	}
}

// GetType implements monitor interface
func (p *StateMonitor) GetType() core.MonitorType {
	return monitorType
}

// InitMonitor implements monitor interface
func (p *StateMonitor) InitMonitor(np types.NodeEventPusher, pw types.ProcessWorker) error {
	if np == nil || pw == nil {
		return fmt.Errorf("both NodeEventPusher and ProcessWorker cannot be nil")
	}

	p.eventPusher = np
	p.worker = pw

	if err := p.UpdateInstances(); err != nil {
		glog.Errorf("update java inances error :%v", err)
	}

	go p.loadAndWatchDailInterface()

	go func() {
		timer := time.NewTimer(5 * time.Second)
	loop:
		for {
			select {
			case <-timer.C:
				glog.V(10).Info("start to update java instances")
				if err := p.UpdateInstances(); err != nil {
					glog.Errorf("update java inances error :%v", err)
				}
				glog.V(10).Info("end to update java instances")
				timer.Reset(5 * time.Second)

			case <-p.closeC:
				glog.V(10).Info("java process monitor stop")
				break loop
			}
		}
	}()
	return nil
}

// StopMonitor implements monitor interface
func (p *StateMonitor) StopMonitor() error {
	p.closeC <- struct{}{}
	close(p.closeC)

	return nil
}

// StartProcess implements monitor interface
func (p *StateMonitor) StartProcess(tsps *core.ClusterReplicaSpec, o io.Writer) error {
	// here we ignore tsps.IntanceNum, as long it is greater than 0
	if tsps.InstancesNum < 0 {
		err := fmt.Errorf("start %v new  %v instance", tsps.InstancesNum, tsps.ClusterName)
		glog.Warningf("%v", err)
		return nil
	}

	project, bin := parseClusterName(string(tsps.ClusterName))
	version := tsps.Version
	cmdType := "start"
	cmd := fmt.Sprintf("%s java %s %s %s %s", config.GetCtrlScriptPath(), cmdType, project, bin, version)
	labels := map[string]string{
		"action":  cmdType,
		"ptype":   "java",
		"cluster": string(tsps.ClusterName),
		"version": version,
		"project": project,
		"bin":     bin,
	}

	fileds := map[string]interface{}{}

	t := types.Task{
		CMD:          cmd,
		Cluster:      tsps.ClusterName,
		Action:       cmdType,
		MetricLabels: labels,
		MetricFileds: fileds,
		Timeout:      time.Minute,
		Deadline:     time.Now().Add(5 * time.Minute),
	}

	p.worker.AddTask(t)

	return nil
}

// StopProcess implements monitor interface
func (p *StateMonitor) StopProcess(ps types.ProcessInfor, o io.Writer) error {
	psinfo, ok := ps.(*ProcessInfos)
	cluster, version := ps.GetClusterNameAndVersion()
	project, bin := parseClusterName(string(cluster))
	if !ok {
		return fmt.Errorf("stop Process %v:%v, process must b java process, got %v",
			cluster, version, ps.GetMonitorType())
	}

	ins := psinfo.jins
	cmdType := "stop"
	parts := strings.Split(string(cluster), core.FieldSperator)
	if len(parts) != 2 {
		err := fmt.Errorf("cluster is expected of form 'a:b', got: %v", cluster)
		glog.Errorf("%v", err)
		return err
	}
	cmd := fmt.Sprintf("%s java %s %s %s %v %v", config.GetCtrlScriptPath(),
		cmdType, project, bin, psinfo.proc.Pid, ins.Version)
	labels := map[string]string{
		"action":  cmdType,
		"ptype":   cmdType,
		"cluster": string(cluster),
		"project": project,
		"version": ins.Version,
		"bin":     bin,
		"user":    ins.User,
	}

	fileds := map[string]interface{}{
		"node": ins.UUID,
	}
	t := types.Task{
		CMD:          cmd,
		Cluster:      cluster,
		Action:       cmdType,
		MetricLabels: labels,
		MetricFileds: fileds,
		Timeout:      time.Minute,
		Deadline:     time.Now().Add(5 * time.Minute),
	}

	psinfo.jins.LifeCycle = core.ILCStopping

	p.worker.AddTask(t)
	return nil
}

// UpdateInstances  update java instance info locally and stored in etcd
// return err if has one
func (p *StateMonitor) UpdateInstances() error {
	var merr *multierror.Error
	// get localhost process lists
	pids, err := psm.GetAllPidsOfType("java")
	if err != nil {
		return err
	}

	pidmap := map[int]struct{}{}
	for _, pid := range pids {
		proc, err := process.NewProcess(int32(pid))

		if err != nil {
			glog.Warningf("get proces start time error %v", err)
			continue
		}
		ctime, _ := proc.CreateTime()
		pidmap[pid] = struct{}{}
		// if  process already  inited
		p.lock.Lock()
		ps, ok := p.pidMap[pid]
		p.lock.Unlock()
		if ok {
			c, err := ps.proc.CreateTime()
			// if they are the some process, and still exist
			if err != nil || c-ctime > 2 || c-ctime < -2 {
				p.lock.Lock()
				delete(p.pidMap, pid)
				delete(p.psState, ps.getInstanceUUID())
				p.lock.Unlock()
			} else {
				ps.jins.UpdateTime = time.Now()
				// if  process already  inited
				continue
			}
		}

		if err != nil {
			merr = multierror.Append(merr, err)
			continue
		}
		// start to calculate cpu usage
		proc.Percent(0)

		user, err := proc.Username()
		if err != nil {
			glog.Errorf("error get project user: %v", err)
		}

		ins := &core.Instance{
			HostID:      hostinfo.GetHostID(),
			Host:        hostinfo.GetHostName(),
			Env:         hostinfo.GetEnv(),
			IP:          p.reportIP,
			ProjecType:  projectType,
			UpdateTime:  time.Now(),
			User:        user,
			ServiceType: core.ServiceUnknown,
			Status:      core.InstanceUnknown,
			LifeCycle:   core.ILCStarting,
		}

		ps = &ProcessInfos{
			proc:             proc,
			jins:             ins,
			state:            nil,
			probStateChanged: true,
		}

		// update instance info
		if err = updateInstanceInfo(pid, proc, ins, true); err != nil {
			merr = multierror.Append(merr, err)
		}

		key := ps.getInstanceUUID()
		p.lock.Lock()
		p.psState[key] = ps
		p.pidMap[pid] = ps
		p.lock.Unlock()

		et := types.NodeCreated
		ne := types.NodeEvent{
			Type:        et,
			ProcessInfo: ps,
		}
		p.eventPusher.PushEvent(ne)
	}

	// remove  stopped process
	if len(pidmap) != len(p.pidMap) {
		p.lock.Lock()
		defer p.lock.Unlock()
		for pid, ps := range p.pidMap {
			if _, ok := pidmap[pid]; !ok {
				ps.probStateChanged = true
				ne := types.NodeEvent{
					Type:        types.NodeStopped,
					ProcessInfo: ps,
				}
				p.eventPusher.PushEvent(ne)
				delete(p.pidMap, pid)
				delete(p.psState, ps.getInstanceUUID())
				p.store.DelClusterInstance(ps.jins)
			}
		}
	}

	return merr.ErrorOrNil()
}

func updateInstanceInfo(pid int,
	p *process.Process,
	jin *core.Instance,
	foreUpdate bool,
) error {
	if p == nil {
		return nil
	}

	if jin == nil {
		return fmt.Errorf("java Instance is nil")
	}

	// if Instance all parsed, skip
	// there maybe an error, if a process status with the same pid
	cts, err := p.CreateTime()
	if err != nil {
		return err
	}
	ctime := time.Unix(cts/1000, 0)
	if jin.Pid != 0 && ctime.Add(2*time.Minute).Before(time.Now()) && !foreUpdate {
		return nil
	}

	jin.Pid = pid
	jin.StartTime = ctime
	jin.UUID = core.UUID(fmt.Sprintf("%s-%v", jin.HostID[:7], pid))

	args, err := p.CmdlineSlice()

	if err != nil {
		return err
	}

	// -Djava.apps.version=51 -Djava.apps.prog=financial-account-server
	for _, v := range args {
		parts := strings.Split(v, "=")
		if len(parts) != 2 {
			continue
		}
		switch parts[0] {
		case "-Djava.apps.version":
			jin.Version = parts[1]
		case "-Djava.apps.prog":
			v := parts[1]
			idx := strings.LastIndex(parts[1], "-")
			if idx == -1 {
				continue
			}
			jin.ClusterName = core.UUID(fmt.Sprintf("%v%v%v", v[:idx], core.FieldSperator, v[idx+1:]))
		case "-Dinstance.sequence":
			v := parts[1]
			jin.Node = v
		}
	}

	// get process listening port
	if jin.ServiceType == core.ServiceUnknown {
		if len(jin.Listening) > 0 {
			jin.ServiceType = core.ServiceService
			return nil
		}

		// default serviceType, alse as an indicator,
		// so we may not execute  following func simultaneously
		jin.ServiceType = core.ServiceDaemon

		go func(jin *core.Instance) {
			ctx := context.Background()
			ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			defer cancel()
			tick := time.NewTicker(2 * time.Second)
			defer tick.Stop()
			for {
				select {
				case <-tick.C:
					addrs, err := psm.ListenPortsOfPid(jin.Pid)
					if err != nil {
						glog.Warningf("get listening port of %v: %v", pid, err)
					}
					if len(addrs) > 0 {
						glog.V(10).Infof("process %v listening: %v", jin.Pid, addrs)
						jin.Listening = addrs
						jin.ServiceType = core.ServiceService
						return
					}
				case <-ctx.Done():
					glog.V(11).Infof("not find listen port for process %v", jin.Pid)
					return
				}
			}
		}(jin)
	}

	return nil
}

// GetProcessPidList implements monitor interface
func (p *StateMonitor) GetProcessPidList() ([]types.ProcessInfor, error) {
	p.lock.Lock()
	defer p.lock.Unlock()
	ret := make([]types.ProcessInfor, len(p.pidMap))
	idx := 0
	for _, ps := range p.pidMap {
		ret[idx] = ps
		idx++
	}

	return ret, nil
}

// ReportInstances instance info to etcd
func (p *StateMonitor) ReportInstances() error {
	pis, err := p.GetProcessPidList()
	if err != nil {
		glog.Errorf("report instance: %v", err)
		return err
	}

	for _, pi := range pis {
		ins := pi.(*ProcessInfos)
		if err := p.store.SaveInstance(ins.jins); err != nil {
			glog.Infof("update java intance info: %v", err)
		}
	}

	return nil
}

// UpdateInstanceStat update instance resource usage on etcd
func (p *StateMonitor) UpdateInstanceStat() error {

	return nil
}

// CleanOldInstances will be call after InitMonitor
func (p *StateMonitor) CleanOldInstances() error {
	ins, err := p.store.ListInstancesOfTypeOnHost(projectType, hostinfo.GetHostID())
	if err != nil {
		glog.Infof("error get instance: %v", err)
		return err
	}
	pis, err := p.GetProcessPidList()
	if err != nil {
		glog.Infof("error get processes: %v", err)
		return err
	}

	for _, pi := range pis {
		p := pi.(*ProcessInfos)
		delete(ins, p.jins.UUID)
	}

	var merr *multierror.Error
	for _, in := range ins {
		glog.Infof("clean old ins, del: %v", in)
		if _, err := p.store.DelClusterInstance(in); err != nil {
			merr = multierror.Append(merr, err)
		}
	}

	return merr.ErrorOrNil()
}

var (
	dialInterfaces = map[core.UUID]map[string]*java.ProbeInterface{}
	projBinMap     = map[string][]string{}
	lock           sync.RWMutex
)

func handlerDialInterface(event watch.Event) error {
	env := hostinfo.GetEnv()
	dat, ok := event.Object.(*java.ProbeConfig)
	if !ok {
		return errors.Errorf("unexpect object type: %T", event.Object)
	}

	lock.Lock()
	defer lock.Unlock()

	// remove old  data
	bins, _ := projBinMap[dat.Project]
	for _, b := range bins {
		cluster := core.UUID(fmt.Sprintf("%v%v%v", dat.Project, core.FieldSperator, b))
		delete(dialInterfaces, cluster)
	}

	switch event.Type {
	case watch.Modified, watch.Added:
		bins = nil
		for bin := range dat.ProbeInterfaces {
			cluster := core.UUID(fmt.Sprintf("%v%v%v", dat.Project, core.FieldSperator, bin))
			ifs := dat.GetProbeInterfaces(env, bin)
			m := map[string]*java.ProbeInterface{}
			for _, i := range ifs {
				m[i.Name] = i
			}
			bins = append(bins, bin)
			dialInterfaces[cluster] = m
		}
		projBinMap[dat.Project] = bins
	}

	return nil
}

func (p *StateMonitor) loadAndWatchDailInterface() error {
	reg := clusters.JavaProbRegister{}

	for {
		ctx := context.Background()
		ctx, cf := context.WithCancel(ctx)
		go func(ctx context.Context, cf context.CancelFunc) {
			time.AfterFunc(2*60*time.Minute, func() { cf() })
			select {
			case <-p.closeC:
				cf()
			case <-ctx.Done():
			}
		}(ctx, cf)

		select {
		case <-p.closeC:
			return nil

		default:
			err := reg.Watch(ctx, handlerDialInterface)
			if err != nil {
				glog.Errorf("java: watch project dialinterface: %v", err)
			}
		}

	}
}

func getDialInterface(cluster core.UUID) map[string]*java.ProbeInterface {
	lock.RLock()
	defer lock.RUnlock()

	return dialInterfaces[cluster]
}

// Probe implements types.ProcessInfo interface
func (psinfo *ProcessInfos) Probe() probe.Result {
	if psinfo == nil || psinfo.jins == nil {
		return probe.Success
	}

	jins := psinfo.jins

	lg := func() interface{} {
		if len(jins.Listening) != 1 {
			return nil
		}

		addr := jins.Listening[0]
		url := fmt.Sprintf("http://%v:%v", addr.IP, addr.Port)
		dis := getDialInterface(jins.ClusterName)
		ret := make([]*pjava.Args, 0, len(dis))
		for _, di := range dis {
			args := pjava.Args{
				Name:    di.Name,
				Cluster: string(jins.ClusterName),
				Data:    strings.NewReader(di.Data),
				URL:     url,
				Headers: di.Header,
			}
			ret = append(ret, &args)
		}

		return ret
	}

	state := psinfo.state

	var conditions, events []*core.Condition
	var ret probe.Result

	if state != nil {
		ret = state.Probe()
		conditions = state.Conditions
		events = state.Events
	}

	// dial interfaces
	if len(jins.Listening) > 0 {
		if len(jins.Listening) != 1 {
			glog.Errorf("java: process havs more than one listening ports: %v", jins.Node)

		} else {
			ret, _, err := pjava.Probe(lg)
			if err != nil {
				glog.Errorf("java probe: %v: %v", jins.ClusterName, err)
			}
			if psinfo.state.ProbState != ret {
				psinfo.state.ProbState = ret
				if ret == probe.Warning {
					events = append(events, &core.Condition{
						Type:    core.ProbError,
						Message: err.Error(),
					})
				} else if ret == probe.Failure {
					conditions = append(conditions, &core.Condition{
						Type:    core.ProbError,
						Message: err.Error(),
					})
				} else if jins.LifeCycle == core.ILCStarting {
					jins.LifeCycle = core.ILCRunning
					psinfo.probStateChanged = true
				}
			}
		}
	}

	jins.Conditions = conditions
	jins.Events = events

	if len(conditions) > 0 {
		ret = probe.Failure
	} else if len(events) > 0 {
		ret = probe.Warning
	}

	return ret
}

// GetNodeID implements ProcessInfor interface
func (psinfo *ProcessInfos) GetNodeID() string {
	return psinfo.jins.Node
}

func (psinfo *ProcessInfos) updateState() psm.ProcessState {
	now := time.Now()
	if psinfo.updateTime.Add(5 * time.Second).Before(now) {
		state := psm.CalProcessState(psinfo.proc)

		state.ClusterName = psinfo.jins.ClusterName
		state.Type = core.MonitorType(psinfo.jins.ProjecType)

		psinfo.jins.ResUsage = &core.InstanceResUsage{
			Memory:         state.MemInfo.RSS,
			CPUTotal:       state.CPUInfo.Total(),
			CPUPercent:     state.CPUPercent,
			Threads:        state.NumThreads,
			DiskBytesRead:  state.DiskIO.ReadBytes,
			DiskBytesWrite: state.DiskIO.WriteBytes,
		}

		if psinfo.state != nil {
			state.PsState = psinfo.state.PsState
			state.ProbState = psinfo.state.ProbState
		}
		psinfo.state = state
		psinfo.updateTime = time.Now()
	}

	return *psinfo.state
}

// GetProcessMetric implements ProcessInfor interface
func (psinfo *ProcessInfos) GetProcessMetric() (metric *types.Metric, err error) {
	state := psinfo.updateState()

	project, bin := parseClusterName(string(psinfo.jins.ClusterName))

	tags := map[string]string{
		"project":   project,
		"bin":       bin,
		"cluster":   string(psinfo.jins.ClusterName),
		"euser":     psinfo.jins.User,
		"node":      psinfo.jins.Node,
		"nodeID":    string(psinfo.jins.UUID),
		"state":     string(state.PsState),
		"probState": string(state.ProbState),
	}

	tags = psm.GetCommonReportTags().Add(tags).ToMap()

	fileds := state.GetMetric()
	if fileds != nil {
		fileds["pid"] = psinfo.proc.Pid
	}

	return &types.Metric{
		Name:   "javaProcessStat",
		Fields: fileds,
		Tags:   tags,
		Time:   psinfo.updateTime,
	}, nil
}

// GetTypeSpecMetrics implements ProcessInfor interface
func (psinfo *ProcessInfos) GetTypeSpecMetrics() (metric *types.Metric, err error) {
	//
	return nil, nil
}

// GetClusterNameAndVersion implements ProcessInfor interface
func (psinfo *ProcessInfos) GetClusterNameAndVersion() (core.UUID, string) {
	jins := psinfo.jins
	return jins.ClusterName, jins.Version
}

func parseClusterName(cluster string) (project, bin string) {
	fields := strings.Split(cluster, core.FieldSperator)
	if len(fields) != 2 {
		// this shouldnot happen
		glog.Fatalf("parse java cluster %v err", cluster)
	}

	project, bin = fields[0], fields[1]
	return
}

// GetMonitorType implements ProcessInfor interface
func (psinfo *ProcessInfos) GetMonitorType() core.MonitorType {
	return monitorType
}

// GetResConsumerEvaluate return value between 0 and 1,  larger value consumer more system resource
func (psinfo *ProcessInfos) GetResConsumerEvaluate() float64 {
	// since this value  in [0, 1], so we need to find a  Reference to compare
	// here we use host total momery and  swap and #threads

	// or we may just use oom-kill scores

	ret := 0.0
	state := psinfo.updateState()
	// memory used percent
	ret = ret + 0.4*float64(state.MemInfo.RSS*1.0/hostinfo.GetMemory())
	ret = ret + 0.1*float64(state.MemInfo.Swap*1.0/hostinfo.GetSwapSize())
	ret = ret + 0.5*float64(state.NumThreads*1.0/65536)

	return ret
}

// ShouldFlush return whether should update node info store on etcd
func (psinfo *ProcessInfos) ShouldFlush() bool {
	return psinfo.probStateChanged
}

// GetReportPath  return relative path to which this node should reported to ectd
func (psinfo *ProcessInfos) getInstanceUUID() string {
	return filepath.Join(string(psinfo.jins.ClusterName), string(psinfo.jins.UUID))
}

var _ types.Monitor = &StateMonitor{}
var _ types.ProcessInfor = &ProcessInfos{}
