package process

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/coreos/fleet/log"
	"github.com/golang/glog"
	"github.com/influxdata/telegraf/internal/hostinfo"
	"github.com/pkg/errors"

	"we.com/jiabiao/common/probe"
	"we.com/jiabiao/monitor/core/java"
	core "we.com/jiabiao/monitor/core/types"
	"we.com/jiabiao/monitor/registry/clusters"
	"we.com/jiabiao/monitor/registry/watch"
)

var (
	k = 1024
	m = 1024 * k
	g = 1024 * m

	low = core.DeployResource{
		Memory:           uint64(128 * m),
		CPU:              uint64(32 * m),
		NetworkIn:        1,
		NetworkOut:       1,
		DiskSpace:        0,
		MaxAllowedMemory: uint64(320 * m),
		MaxAllowdThreads: 1 * k,
	}

	medium = core.DeployResource{
		Memory:           uint64(256 * m),
		CPU:              uint64(32 * m),
		NetworkIn:        1,
		NetworkOut:       1,
		DiskSpace:        1,
		MaxAllowedMemory: uint64(650 * m),
		MaxAllowdThreads: 2 * k,
	}

	high = core.DeployResource{
		Memory:           uint64(2048 * m),
		CPU:              uint64(256 * m),
		NetworkIn:        1,
		NetworkOut:       1,
		DiskSpace:        1,
		MaxAllowedMemory: uint64(4 * g),
		MaxAllowdThreads: 2 * k,
	}

	// defaultResUsage default resource uage of an instance of type of env
	defaultResUsage = map[core.MonitorType]map[core.ENV]core.DeployResource{
		core.MonitorType(java.Type): map[core.ENV]core.DeployResource{
			core.ENV("test"): low,
			core.ENV("dev"):  low,
			core.ENV("uat"):  medium,
			core.ENV("call"): high,
			core.ENV("idc"):  high,
		},
	}

	defalutMaxAllowThreads = 3 * k / 2
)

type diskIOInfo struct {
	Time  time.Time
	Count uint64
}

var (
	stopC         chan struct{}
	lock          sync.RWMutex
	startd        bool
	diskLock      sync.RWMutex
	lastDiskWrite = map[int]*diskIOInfo{}
	deployConfigs = map[core.UUID]*core.DeployConfig{}
	deployBinMap  = map[string][]string{}
)

// Start starts  load deploy config from etcd, so cannot be used later
func Start() {
	lock.Lock()
	defer lock.Unlock()

	if startd {
		return
	}
	startd = true
	stopC = make(chan struct{})
	go loadAndWatchDeployConfig()
}

// Stop stops watch etcd deploy config changes
func Stop() {
	lock.Lock()
	defer lock.Unlock()
	if !startd {
		return
	}
	close(stopC)
	startd = false
}

func handlerDeployConfig(event watch.Event) error {
	dat, ok := event.Object.(*core.DeployConfig)
	if !ok {
		return errors.Errorf("unexpect object type: %T", event.Object)
	}

	lock.Lock()
	defer lock.Unlock()

	switch event.Type {
	case watch.Added, watch.Modified:
		resReq := dat.ResourceRequired
		if resReq.MaxAllowedMemory == 0 {
			resReq.MaxAllowedMemory = 2 * resReq.Memory
		}

		if resReq.MaxAllowedCPU == 0 {
			resReq.MaxAllowedCPU = uint64(hostinfo.GetNumOfCPUs() * 1024 * 1024 * 1024 / 4)
		}

		if resReq.MaxAllowdThreads == 0 {
			resReq.MaxAllowdThreads = 4096
		}

		deployConfigs[dat.Cluster] = dat
	case watch.Deleted:
		delete(deployConfigs, dat.Cluster)
	}
	return nil
}

func loadAndWatchDeployConfig() error {
	env := hostinfo.GetEnv()
	reg := clusters.NewRegistry(env)
	defer reg.Stop()

	for {
		ctx := context.Background()
		ctx, cf := context.WithCancel(ctx)
		go func(ctx context.Context, cf context.CancelFunc) {
			time.AfterFunc(2*60*time.Minute, func() { cf() })
			select {
			case <-stopC:
				cf()
			case <-ctx.Done():
			}
		}(ctx, cf)

		select {
		case <-stopC:
			return nil

		default:
			err := reg.WatchDeployConfig(handlerDeployConfig)
			if err != nil {
				glog.Errorf("java: watch project dialinterface: %v", err)
			}
		}

	}
}

func getDeployConfig(cluster core.UUID) *core.DeployConfig {
	lock.RLock()
	if !startd {
		glog.Fatal("process status: deploy config not started")
	}
	dc := deployConfigs[cluster]
	if dc == nil {
		log.Warningf("java: no deploy config for %v", cluster)
	}
	lock.RUnlock()

	return dc
}

// Probe  check process resource usage
func (ps *ProcessState) Probe() (probe.Result, string) {
	if ps == nil {
		return probe.Success, ""
	}

	var events []*core.Condition
	var conditions []*core.Condition
	cluster := ps.ClusterName
	dc := getDeployConfig(cluster)
	var resReq core.DeployResource

	if dc == nil {
		resReq = medium
		tpCfg, ok := defaultResUsage[ps.Type]
		if ok {
			rs, ok := tpCfg[hostinfo.GetEnv()]
			if ok {
				resReq = rs
			}
		}
	} else {
		resReq = dc.ResourceRequired
		if resReq.MaxAllowdThreads == 0 {
			resReq.MaxAllowdThreads = defalutMaxAllowThreads
		}
	}

	resAct := &core.InstanceResUsage{
		Memory:         ps.MemInfo.RSS,
		CPUPercent:     ps.CPUPercent,
		Threads:        ps.NumThreads,
		DiskBytesWrite: ps.DiskIO.WriteBytes,
	}

	ratio := resAct.Memory * 100 / resReq.Memory
	switch {
	case ratio > 150 && resAct.Memory <= resReq.MaxAllowedMemory:
		events = append(events, &core.Condition{
			Type:    core.HighMem,
			Message: fmt.Sprintf("memory usage: %v, %v%%  of configed", resAct.Memory, ratio),
		})
	case resReq.Memory >= resReq.MaxAllowedMemory:
		conditions = append(conditions, &core.Condition{
			Type:    core.HighMem,
			Message: fmt.Sprintf("memory usage: %v, greater than configed: %v", resAct.Memory, resReq.MaxAllowedMemory),
		})
	}

	if resAct.Threads > resReq.MaxAllowdThreads {
		conditions = append(conditions, &core.Condition{
			Type:    core.HighThreads,
			Message: fmt.Sprintf("threads: %v, greater than  configed: %v", resAct.Threads, resReq.MaxAllowdThreads),
		})
	} else if resAct.Threads > resReq.MaxAllowdThreads*8/10 {
		events = append(events, &core.Condition{
			Type:    core.HighThreads,
			Message: fmt.Sprintf("threads: %v, greater than 80%% configed: %v", resAct.Threads, resReq.MaxAllowdThreads),
		})
	}

	g := 1024 * 1024 * 1024
	cpuUsage := uint64(resAct.CPUPercent * float64(g))
	if cpuUsage > resReq.MaxAllowedCPU {
		conditions = append(conditions, &core.Condition{
			Type:    core.HighCPU,
			Message: fmt.Sprintf("cpu: %.2f%% greater than max allowed: %.2f%%", (resAct.CPUPercent * 100), float64(resReq.MaxAllowedCPU)/float64(g)),
		})
	} else if cpuUsage > resReq.MaxAllowedCPU*8/10 || cpuUsage > resReq.CPU*5 {
		events = append(events, &core.Condition{
			Type:    core.HighCPU,
			Message: fmt.Sprintf("cpu: %.2f%% greater than configed: %.2f%%", (resAct.CPUPercent * 100), float64(resReq.CPU)/float64(g)),
		})
	}

	diskLock.RLock()
	diskInfo, ok := lastDiskWrite[ps.Pid]
	diskLock.RUnlock()

	renew := true
	if ok {
		day := uint64(12 * 24)
		d := time.Now().Sub(diskInfo.Time).Seconds()
		size := resAct.DiskBytesWrite - diskInfo.Count
		// in 5mins
		if d < 300 {
			if size*day > resReq.DiskSpace {
				events = append(events, &core.Condition{
					Type:    core.HighDiskIO,
					Message: fmt.Sprintf("diskIO: write %v", resAct.DiskBytesWrite),
				})
			} else {
				renew = false
			}
		}
	}

	if renew {
		di := &diskIOInfo{
			Time:  time.Now(),
			Count: resAct.DiskBytesWrite,
		}
		diskLock.Lock()
		lastDiskWrite[ps.Pid] = di
		diskLock.Unlock()
	}

	ps.Conditions = conditions
	ps.Events = events
	ps.PsState = PSRunning
	ret := probe.Success

	var reason string

	if len(events) > 0 {
		ps.PsState = PsState(events[0].Type)
		ret = probe.Warning
		for _, e := range events {
			reason = fmt.Sprintf("%v; %v: %v", reason, e.Type, e.Message)
		}
	}

	if len(conditions) > 0 {
		ps.PsState = PsState(conditions[0].Type)
		ret = probe.Failure
		reason = ""
		for _, c := range conditions {
			reason = fmt.Sprintf("%v; %v: %v", reason, c.Type, c.Message)
		}
	}
	return ret, strings.TrimLeft(reason, "; ")
}
