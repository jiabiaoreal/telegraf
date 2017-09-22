package ps

import (
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/golang/glog"
	"github.com/pkg/errors"
	"github.com/shirou/gopsutil/process"
)

type ps struct {
	typeMatch  map[string]*PidType
	procs      map[int]empty
	types      map[string]map[int]*process.Process
	updateTime time.Time
	lock       sync.RWMutex
	cacheTime  time.Duration
}
type empty struct{}

// update all running process
func (p *ps) update(force bool) {
	if p.updateTime.Add(p.cacheTime).After(time.Now()) && !force {
		return
	}

	pids, err := Pgrep(".", false)
	if err != nil {
		glog.Warningf("get ps: %v", err)
	}

	prcs := make(map[int]empty, len(pids))
	for _, v := range pids {
		prcs[v] = empty{}
	}

	p.procs = prcs
	p.updateTime = time.Now()

	p.classifyProcs()
}

// caller should hold the lock
func (p *ps) classifyProcs() {
	types := map[string]map[int]*process.Process{}

	for k, v := range p.types {
		typmap := map[int]*process.Process{}
		types[k] = typmap
		for pid, proc := range v {
			// TODO(jiabiao): better way to test process still running?
			err := proc.SendSignal(syscall.Signal(0))
			if err == nil {
				typmap[pid] = proc
			}
			delete(p.procs, pid)
		}
	}

	unknownMap, ok := types[unknown]
	if !ok {
		unknownMap = map[int]*process.Process{}
		types[unknown] = unknownMap
	}

	// new started process
out:
	for pid := range p.procs {
		proc, err := process.NewProcess(int32(pid))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			glog.Warningf("ps: get system running processes: %v", err)
			continue
		}

		cmdline, err := proc.Cmdline()
		if err != nil {
			glog.Warningf("ps: get process cmdline: %v", err)
			continue
		}

		for k, v := range p.typeMatch {
			typeMap, ok := types[k]
			if !ok {
				typeMap = map[int]*process.Process{}
				types[k] = typeMap
			}
			if matchCmdline(cmdline, v) {
				typeMap[pid] = proc
				continue out
			}
		}
		unknownMap[pid] = proc
	}

	p.types = types
}

// GetProcsOfType returns process of typ
func (p *ps) GetProcsOfType(typ string, forceUpdate bool) (map[int]*process.Process, error) {
	p.lock.Lock()
	defer p.lock.Unlock()
	// update if  needed
	p.update(forceUpdate)

	tmp, ok := p.types[typ]

	if !ok {
		if _, ok := p.typeMatch[typ]; !ok {
			return nil, errors.New("unknown typ")
		}

		return nil, nil
	}

	ret := make(map[int]*process.Process, len(tmp))
	for k, v := range tmp {
		ret[k] = v
	}

	return ret, nil
}

var (
	psManager *ps
)

// Scanner scan  the system for process of a certain type
type Scanner interface {
	// GetProcsOfType  most of the times, forceUpdate should set to force
	// defaut processes will be cached for cacheTime
	// typ is specified when setup
	GetProcsOfType(typ string, forceUpdate bool) (map[int]*process.Process, error)
}

// Setup create a global PsScanner
// setup cannot be called more than one times, to change the config
func Setup(matches map[string]*PidType, cacheTime time.Duration) (Scanner, error) {
	for _, v := range matches {
		if _, err := v.GetRegexp(); err != nil {
			return nil, err
		}
	}

	if psManager == nil {
		psManager = &ps{}
	}

	psManager.lock.Lock()
	defer psManager.lock.Unlock()

	psManager.typeMatch = matches
	psManager.cacheTime = cacheTime

	return psManager, nil
}
