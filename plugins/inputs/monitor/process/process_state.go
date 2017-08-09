package process

import (
	"time"

	multierror "github.com/hashicorp/go-multierror"
	"github.com/influxdata/telegraf/internal/hostinfo"
	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/net"
	"github.com/shirou/gopsutil/process"
	"we.com/jiabiao/common/probe"
)

// PsState  process State
type PsState string // process state
const (
	// PsState
	// PSUnknown  state unknown
	PSUnknown PsState = "unknown"
	//PSRunning process running
	PSRunning PsState = "running"
	// PSStopping process is about to stop or is stopping
	PSStopping PsState = "stopping"
	// PSStopped process has stopped
	PSStopped PsState = "stopped"
)

// ProcessState common process state info
type ProcessState struct {
	Pid        int
	NumFDs     int
	NumThreads int
	CPUPercent float64
	UpdateTime time.Time
	StopTime   time.Time // assumed stop time, if passed, process is still not stopped, try to stop it again
	PsState    PsState
	ProbState  probe.Result

	CtxSwitch process.NumCtxSwitchesStat
	MemInfo   process.MemoryInfoStat
	NetIO     []net.IOCountersStat // currently cannot get per process netio info, just leave this empty
	DiskIO    process.IOCountersStat
	CPUInfo   cpu.TimesStat
}

// CalProcessState get common state and metric of a process
func CalProcessState(proc *process.Process) (ps *ProcessState) {
	if proc == nil {
		return
	}
	var merr *multierror.Error
	numFDs, err := proc.NumFDs()
	if err != nil {
		merr = multierror.Append(merr, err)
	}

	numThread, err := proc.NumThreads()
	if err != nil {
		merr = multierror.Append(merr, err)
	}

	cpuPercent, err := proc.Percent(0)
	if err != nil {
		merr = multierror.Append(merr, err)
	}

	ps = &ProcessState{
		Pid:        int(proc.Pid),
		NumFDs:     int(numFDs),
		NumThreads: int(numThread),
		CPUPercent: cpuPercent,
		UpdateTime: time.Now(),
	}

	cpuInfo, err := proc.Times()
	if err != nil {
		merr = multierror.Append(merr, err)
	} else {
		ps.CPUInfo = *cpuInfo
	}

	ctxSwitch, err := proc.NumCtxSwitches()
	if err != nil {
		merr = multierror.Append(merr, err)
	} else {
		ps.CtxSwitch = *ctxSwitch
	}

	memInfo, err := proc.MemoryInfo()
	if err != nil {
		merr = multierror.Append(merr, err)
	} else {
		ps.MemInfo = *memInfo
	}

	diskIO, err := proc.IOCounters()
	if err != nil {
		merr = multierror.Append(merr, err)
	} else {
		ps.DiskIO = *diskIO
	}

	return
}

// GetMetric return metric for current ProcessState
func (ps *ProcessState) GetMetric() map[string]interface{} {
	metric := map[string]interface{}{}
	metric = map[string]interface{}{
		"numFDs":     ps.NumFDs,
		"numThreads": ps.NumThreads,
		"cpuPercent": ps.CPUPercent,
		"memRss":     ps.MemInfo.RSS,
		"memSwap":    ps.MemInfo.Swap,
		"memVms":     ps.MemInfo.VMS,

		"diskReadBytes":  ps.DiskIO.ReadBytes,
		"diskReadCount":  ps.DiskIO.ReadCount,
		"diskWriteBytes": ps.DiskIO.WriteBytes,
		"diskWriteCount": ps.DiskIO.WriteCount,

		"cpuUser":   ps.CPUInfo.User,
		"cpuSys":    ps.CPUInfo.System,
		"cpuIOWait": ps.CPUInfo.Iowait,
	}

	return metric
}

type ReportTags map[string]string

func GetCommonReportTags() ReportTags {
	ret := map[string]string{
		"hostid":   string(hostinfo.GetHostID()),
		"hostname": hostinfo.GetHostName(),
		"env":      string(hostinfo.GetEnv()),
	}

	return ReportTags(ret)
}

func (rt ReportTags) Add(labels map[string]string) ReportTags {
	for k, v := range labels {
		rt[k] = v
	}

	return rt
}

func (rt ReportTags) ToMap() map[string]string {
	return map[string]string(rt)
}
