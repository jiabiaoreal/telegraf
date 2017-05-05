package process

import (
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"

	"we.com/jiabiao/common/alert"

	"github.com/armon/circbuf"
	"github.com/golang/glog"
	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs/monitor/output"
	"github.com/influxdata/telegraf/plugins/inputs/monitor/types"
	"github.com/mitchellh/go-linereader"
)

const (
	defaultMaxBufSize = 64 * 1024
	defaultTimeout    = 2 * time.Minute
)

func execLocal(command string, out io.Writer, timeout time.Duration) error {
	// Execute the command using a shell
	var shell, flag string
	shell = "/bin/bash"
	flag = "-c"

	// Setup the reader that will read the lines from the command
	pr, pw := io.Pipe()
	copyDoneCh := make(chan struct{})
	go copyOutput(out, pr, copyDoneCh)

	glog.V(5).Infof("start to execute command: %v", command)
	// Setup the command
	cmd := exec.Command(shell, flag, command)
	output, _ := circbuf.NewBuffer(defaultMaxBufSize)
	cmd.Stderr = io.MultiWriter(output, pw)
	cmd.Stdout = io.MultiWriter(output, pw)

	// log what we're about to run
	glog.Infof("Executing: %s %s \"%s\"",
		shell, flag, command)

	// Run the command to completion
	err := cmd.Run()

	// Close the write-end of the pipe so that the goroutine mirroring output
	// ends properly.
	pw.Close()
	<-copyDoneCh

	if err != nil {
		return fmt.Errorf("Error running command '%s': %v. Output: %s",
			command, err, output.Bytes())
	}

	return nil
}

func copyOutput(o io.Writer, r io.Reader, doneCh chan<- struct{}) {
	defer close(doneCh)
	lr := linereader.New(r)
	for line := range lr.Ch {
		o.Write([]byte(line))
	}
}

type histNode struct {
	AddTime time.Time
	cluster string
	action  string
}

// StartStopWorker to start stop proccess in the background
type StartStopWorker struct {
	taskChan chan types.Task
	stopChan chan struct{}
	history  []*histNode
	hisLock  sync.RWMutex
	acc      telegraf.Accumulator
}

// NewStartStopWorker create a new Worker
func NewStartStopWorker(acc telegraf.Accumulator) *StartStopWorker {
	worker := &StartStopWorker{
		taskChan: make(chan types.Task, 8),
		acc:      acc,
		stopChan: make(chan struct{}),
	}
	go worker.run()
	go worker.cleanHistory()

	return worker
}

func (ssw *StartStopWorker) cleanHistory() {
	ticker := time.NewTicker(30 * time.Second)
	for {
		select {
		case now := <-ticker.C:
			ssw.hisLock.Lock()
			for idx, n := range ssw.history {
				if n.AddTime.Add(time.Hour).Before(now) {
					continue
				}
				var hn []*histNode
				ssw.history = append(hn, ssw.history[idx:]...)
				break
			}
			ssw.hisLock.Unlock()

			stat := map[string]int{}
			ssw.hisLock.RLock()
			for _, n := range ssw.history {
				key := fmt.Sprintf("%v %v", n.cluster, n.action)
				v, ok := stat[key]
				if !ok {
					v = 0
					stat[key] = v
				}
				stat[key] = v + 1
			}
			ssw.hisLock.RUnlock()

			// if any cluster action happend more than 4 times,  alert
			for k, v := range stat {
				if v > 4 {
					msg := fmt.Sprintf("service %v  %v times in an hour", k, v)
					alert.SendMsg(msg)
				}
			}
		}
	}
}

// Stop stops the worker
func (ssw *StartStopWorker) Stop() {
	close(ssw.stopChan)
}

// AddTask add a job to execute
func (ssw *StartStopWorker) AddTask(t types.Task) {
	ssw.taskChan <- t
}

func (ssw *StartStopWorker) run() {
	lop := output.NewLogOutputer(defaultMaxBufSize)

loop:
	for {
		select {
		case t := <-ssw.taskChan:
			now := time.Now()

			tags := t.MetricLabels

			metric := t.MetricFileds

			tags = GetCommonReportTags().Add(tags).ToMap()

			if !t.Deadline.IsZero() && now.After(t.Deadline) {
				metric["event"] = "skipped"
				ssw.acc.AddFields("process_event", metric, tags, now)
				glog.Infof("skip job %v for deadline exceeded", t)
				continue
			}

			glog.Infof("start to execut job %v", t)
			metric["event"] = "start"
			ssw.acc.AddFields("process_event", metric, tags)
			timeout := defaultTimeout
			if t.Timeout > 0 {
				timeout = t.Timeout
			}

			ssw.hisLock.Lock()
			ssw.history = append(ssw.history, &histNode{
				AddTime: time.Now(),
				cluster: string(t.Cluster),
				action:  t.Action,
			})
			ssw.hisLock.Unlock()

			err := execLocal(t.CMD, lop, timeout)

			if err != nil {
				metric["event"] = "failed"
				ssw.acc.AddFields("process_event", metric, tags)
				glog.Warningf("%v", err)
			}
			metric["event"] = "end"
			ssw.acc.AddFields("process_event", metric, tags)
			glog.Infof("jobs execute out: %q", lop.GetDataString())
			// reset buf
			lop.Reset()

		case <-ssw.stopChan:
			close(ssw.taskChan)
			break loop

		}
	}
}
