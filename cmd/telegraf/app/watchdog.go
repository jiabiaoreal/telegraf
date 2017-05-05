// +build !windows

package app

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/golang/glog"
	multierror "github.com/hashicorp/go-multierror"
	"github.com/influxdata/telegraf/plugins/inputs/monitor/process"
	"github.com/pkg/errors"
)

const (
	rebornEnv   = "_reborn"
	rebornValue = "shouldStart"
)

type Watchdog struct {
	Ready                    chan bool
	RestartChildC            chan bool
	ReqStopWatchdog          chan bool
	TermChildAndStopWatchdog chan bool
	reborn                   chan bool
	Done                     chan bool
	CurrentPid               chan int
	curPid                   int

	startCount         int64
	mut                sync.Mutex
	shutdown           bool
	lastChildStartTime time.Time

	PathToChildExecutable string
	Args                  []string
	Attr                  os.ProcAttr
	err                   error
	needRestart           bool
	proc                  *os.Process
	exitAfterReaping      bool
}

// NewWatchdog creates a Watchdog structure but
// does not launch it or its child process until
// Start() is called on it.
// The attr argument sets function attributes such
// as environment and open files; see os.ProcAttr for details.
// Also, attr can be nil here, in which case and
// empty os.ProcAttr will be supplied to the
// os.StartProcess() call.
func NewWatchdog(
	attr *os.ProcAttr,
	pathToChildExecutable string,
	args ...string) *Watchdog {

	cpOfArgs := make([]string, 0, len(args))
	for i := range args {
		cpOfArgs = append(cpOfArgs, args[i])
	}

	w := &Watchdog{
		PathToChildExecutable: pathToChildExecutable,
		Args:                     cpOfArgs,
		Ready:                    make(chan bool),
		RestartChildC:            make(chan bool, 1),
		ReqStopWatchdog:          make(chan bool),
		TermChildAndStopWatchdog: make(chan bool),
		reborn:     make(chan bool),
		Done:       make(chan bool),
		CurrentPid: make(chan int),
	}

	if attr != nil {
		w.Attr = *attr
	}
	return w
}

// StartAndWatch is the convenience/main entry API.
// pathToProcess should give the path to the executable within
// the filesystem. If it dies it will be restarted by
// the Watchdog.
func StartAndWatch(pathToProcess string, args ...string) (*Watchdog, error) {

	// start our child; restart it if it dies.
	watcher := NewWatchdog(nil, pathToProcess, args...)
	watcher.Start()

	return watcher, nil
}

func (w *Watchdog) AlreadyDone() bool {
	select {
	case <-w.Done:
		return true
	default:
		return false
	}
}

func (w *Watchdog) Stop() error {
	if w.AlreadyDone() {
		// once Done, w.err is immutable, so we don't need to lock.
		return w.err
	}
	w.mut.Lock()
	if w.shutdown {
		defer w.mut.Unlock()
		return w.err
	}
	w.mut.Unlock()

	close(w.ReqStopWatchdog)
	<-w.Done
	// don't wait for Done while holding the lock,
	// as that is deadlock prone.

	w.mut.Lock()
	defer w.mut.Unlock()
	w.shutdown = true
	return w.err
}

func (w *Watchdog) SetErr(err error) {
	w.mut.Lock()
	defer w.mut.Unlock()
	w.err = err
}

func (w *Watchdog) GetErr() error {
	w.mut.Lock()
	defer w.mut.Unlock()
	return w.err
}

// see w.err for any error after w.Done
func (w *Watchdog) Start() {

	signalChild := make(chan os.Signal, 1)

	signal.Notify(signalChild, syscall.SIGCHLD)

	otherPids, err := FindProcesses()
	if err != nil {
		glog.Fatalf("find current running pids of: %v", err)
	}
	glog.V(10).Infof("current running pids except current process: %v", otherPids)
	// watchdog restart
	reborn := os.Getenv(rebornEnv)
	if len(otherPids) > 0 && reborn != rebornValue {
		glog.Fatalf("process with pid: %v, already running", otherPids)
	} else {
		// wait previous  watchdog to exit
		tick := time.NewTicker(100 * time.Millisecond)
		timer := time.NewTimer(5 * time.Second)
		oldProcs := []*os.Process{}
		for _, pid := range otherPids {
			proc, _ := os.FindProcess(pid)
			oldProcs = append(oldProcs, proc)
		}
	waiting:
		for {
			select {
			case <-tick.C:
				rem := []*os.Process{}
				for _, proc := range oldProcs {
					if err := proc.Signal(syscall.Signal(0)); err != nil {
						rem = append(rem, proc)
					}
				}
				oldProcs = rem
				if len(oldProcs) == 0 {
					break waiting
				}
			case <-timer.C:
				glog.Error("waiting old watchdog exit timeout, just kill")
				pids := []string{}
				var merr *multierror.Error
				for _, proc := range oldProcs {
					if err := proc.Kill(); err != nil {
						merr = multierror.Append(merr, err)
					}
					pids = append(pids, strconv.Itoa(proc.Pid))
				}

				glog.Errorf("killed: %v, err: %v", pids, merr.ErrorOrNil())
			}
		}
	}

	w.needRestart = true
	var ws syscall.WaitStatus
	go func() {
		defer func() {
			if w.proc != nil {
				w.proc.Release()
			}
			close(w.Done)
			// can deadlock if we don't close(w.Done) before grabbing the mutex:
			w.mut.Lock()
			w.shutdown = true
			w.mut.Unlock()
			signal.Stop(signalChild) // reverse the effect of the above Notify()
		}()
		var err error

	reaploop:
		for {
			if w.needRestart {
				if w.proc != nil {
					w.proc.Release()
				}
				secondToSleep := w.lastChildStartTime.Unix() + 10 - time.Now().Unix()
				if secondToSleep > 0 {
					glog.Infof("since last time start child process is not %v second ago, we try to sleep %v seconds.", 10-secondToSleep, secondToSleep)
				wait:
					select {
					case <-time.After(time.Duration(secondToSleep) * time.Second):
						break
					case <-w.ReqStopWatchdog:
						glog.Info("received stop sigal, exiting")
						return
					case <-w.RestartChildC:
						goto wait
					}
				}

				glog.V(10).Infof(" debug: about to start '%s %v'", w.PathToChildExecutable, w.Args)
				w.proc, err = os.StartProcess(w.PathToChildExecutable, append(w.Args, "-watchdog=false"), &w.Attr)
				if err != nil {
					w.err = err
					return
				}
				w.curPid = w.proc.Pid
				w.needRestart = false
				w.startCount++
				w.lastChildStartTime = time.Now()
				glog.Infof("Start number %d: Watchdog started pid %d / new process '%s'", w.startCount, w.proc.Pid, w.PathToChildExecutable)
			}

			select {
			case w.CurrentPid <- w.curPid:
			case <-w.TermChildAndStopWatchdog:
				glog.Info("TermChildAndStopWatchdog noted, exiting watchdog.Start() loop")

				err := w.proc.Signal(syscall.SIGKILL)
				if err != nil {
					err = fmt.Errorf("warning: watchdog tried to SIGKILL pid %d but got error: '%s'", w.proc.Pid, err)
					w.SetErr(err)
					log.Printf("%s", err)
					return
				}
				w.exitAfterReaping = true
				continue reaploop
			case <-w.ReqStopWatchdog:
				glog.Info("ReqStopWatchdog noted, exiting watchdog.Start() loop")
				return
			case <-w.RestartChildC:
				glog.V(10).Info("got <-w.RestartChildC")
				err := w.proc.Signal(syscall.SIGKILL)
				if err != nil {
					err = fmt.Errorf("warning: watchdog tried to SIGKILL pid %d but got error: '%s'", w.proc.Pid, err)
					w.SetErr(err)
					log.Printf("%s", err)
					return
				}
				w.curPid = 0
				continue reaploop
			case <-w.reborn:
				// kill child and start a new watch and than exits
				defer func() {
					glog.Infof("start a new watchdog")
					w.Attr.Env = append(w.Attr.Env, fmt.Sprintf("%v=%v", rebornEnv, rebornValue))
					w.proc, err = os.StartProcess(w.PathToChildExecutable, w.Args, &w.Attr)
					if err != nil {
						glog.Errorf("start new watchdog failed: %v", err)
					}
				}()

				glog.V(10).Info("reborn: stop child process")
				err := w.proc.Signal(syscall.SIGKILL)
				if err != nil {
					err = fmt.Errorf("warning: watchdog tried to SIGKILL pid %d but got error: '%s'", w.proc.Pid, err)
					w.SetErr(err)
					log.Printf("%s", err)
				}
				// exit
				return

			case <-signalChild:
				glog.V(10).Info(" got <-signalChild")

				for i := 0; i < 1000; i++ {
					pid, err := syscall.Wait4(w.proc.Pid, &ws, syscall.WNOHANG, nil)
					// pid > 0 => pid is the ID of the child that died, but
					//  there could be other children that are signalling us
					//  and not the one we in particular are waiting for.
					// pid -1 && errno == ECHILD => no new status children
					// pid -1 && errno != ECHILD => syscall interupped by signal
					// pid == 0 => no more children to wait for.
					glog.Infof("pid=%v  ws=%v and err == %v", pid, ws, err)
					switch {
					case err != nil:
						err = fmt.Errorf("wait4() got error back: '%s' and ws:%v", err, ws)
						log.Printf("warning in reaploop, wait4(WNOHANG) returned error: '%s'. ws=%v", err, ws)
						w.SetErr(err)
						continue reaploop
					case pid == w.proc.Pid:
						glog.Infof(" Watchdog saw OUR current w.proc.Pid %d/process '%s' finish with waitstatus: %v.", pid, w.PathToChildExecutable, ws)
						if w.exitAfterReaping {
							glog.Infof("watchdog sees exitAfterReaping. exiting now.")
							return
						}
						w.needRestart = true
						w.curPid = 0
						continue reaploop
					case pid == 0:
						// this is what we get when SIGSTOP is sent on OSX. ws == 0 in this case.
						// Note that on OSX we never get a SIGCONT signal.
						// Under WNOHANG, pid == 0 means there is nobody left to wait for,
						// so just go back to waiting for another SIGCHLD.
						glog.Infof("pid == 0 on wait4, (perhaps SIGSTOP?): nobody left to wait for, keep looping. ws = %v", ws)
						continue reaploop
					default:
						glog.Warningf("warning in reaploop: wait4() negative or not our pid, sleep and try again")
						time.Sleep(time.Millisecond)
					}
				} // end for i
				w.SetErr(fmt.Errorf("could not reap child PID %d or obtain wait4(WNOHANG)==0 even after 1000 attempts", w.proc.Pid))
				log.Printf("%s", w.err)
				return
			} // end select
		} // end for reaploop
	}()
}

func (w *Watchdog) RestartChild() {
	w.RestartChildC <- true
}

// start a new watchdog and then stop
func (w *Watchdog) Reborn() {
	w.reborn <- true
}

var watchConfig = flag.Bool("watch-Update", true,
	"watch etcd for update config change, required watchdog to be true")
var watchDog = flag.Bool("watchdog", true,
	"start watchdog to start process when exited")

var wd *Watchdog
var watchdogVersion = ""
var executablePath = ""

func init() {
	exePath, err := os.Executable()
	if err != nil {
		glog.Fatal(err)
	}
	executablePath = exePath
}

func Start() bool {
	if !*watchDog {
		return false
	}

	args := append([]string{}, os.Args...)
	nargs := []string{}
	for _, arg := range args {
		parts := strings.Split(arg, "=")
		tmp := strings.TrimSpace(parts[0])
		if strings.Contains(tmp, "watchdog") || strings.Contains(tmp, "watch-config") {
			continue
		}
		nargs = append(nargs, arg)
	}

	glog.Infof("exePath: %v, args: %v", executablePath, nargs)

	prop := &os.ProcAttr{
		Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
		Env:   os.Environ(),
		Sys: &syscall.SysProcAttr{
			Setpgid: true,
		},
	}

	wd = NewWatchdog(prop, executablePath, nargs...)
	wd.Start()

	if *watchConfig {
		updater, err := NewUpdater()
		if err != nil {
			glog.Fatal(err)
		}

		go updater.Start()
	}

	return true
}

func StopWatch() {
	if wd != nil {
		wd.Stop()
	}
}

func StopChildAndExit() {
	if wd == nil {
		return
	}
	wd.TermChildAndStopWatchdog <- true
	wd.Stop()
}

func Restart(childOnly bool) {
	if wd == nil {
		return
	}
	if childOnly {
		wd.RestartChild()
	} else {
		wd.Reborn()
	}
}

func Done() <-chan bool {
	if wd == nil {
		ch := make(chan bool)
		close(ch)
		return ch
	}

	return wd.Done
}

// FindProcesses find current running pids of these binary file
// including watchdogs and agents, but execute current process
func FindProcesses() ([]int, error) {
	curPids, err := process.Pgrep(filepath.Base(executablePath), true)
	if err != nil {
		return nil, errors.Wrap(err, "find process pids of agent")
	}

	otherPids := []int{}
	curPid := os.Getpid()
	// get actual program name
	a, err := filepath.EvalSymlinks(executablePath)
	if err != nil {
		a = executablePath
	}

	for _, pid := range curPids {
		p, _ := filepath.EvalSymlinks(fmt.Sprintf("/proc/%v/exe", pid))
		if pid != curPid && p == a {
			otherPids = append(otherPids, pid)
		}
	}

	return otherPids, nil
}
