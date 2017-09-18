package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	_ "net/http/pprof" // Comment this line to disable pprof endpoint.
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"we.com/jiabiao/common/etcd"
	"we.com/jiabiao/monitor/registry/generic"

	"github.com/golang/glog"
	"github.com/influxdata/telegraf/agent"
	"github.com/influxdata/telegraf/cmd/telegraf/app"
	"github.com/influxdata/telegraf/internal/config"
	"github.com/influxdata/telegraf/internal/hostinfo"
	"github.com/influxdata/telegraf/logger"
	_ "github.com/influxdata/telegraf/plugins/aggregators/all"
	"github.com/influxdata/telegraf/plugins/inputs"
	_ "github.com/influxdata/telegraf/plugins/inputs/all"
	"github.com/influxdata/telegraf/plugins/outputs"
	_ "github.com/influxdata/telegraf/plugins/outputs/all"
	_ "github.com/influxdata/telegraf/plugins/processors/all"
	"github.com/pkg/errors"
)

var pprofAddr = flag.String("pprof-addr", "",
	"pprof address to listen on, not activate pprof if empty")
var fTest = flag.Bool("test", false, "gather metrics, print them out, and exit")
var fConfig = flag.String("config", "", "configuration file to load")
var fConfigDirectory = flag.String("config-directory", "",
	"directory containing additional *.conf files")
var fVersion = flag.Bool("version", false, "display the version")
var fSampleConfig = flag.Bool("sample-config", false,
	"print out full sample configuration")
var fPidfile = flag.String("pidfile", "/var/run/telegraf.pid", "file to write our pid to")
var fInputFilters = flag.String("input-filter", "",
	"filter the inputs to enable, separator is :")
var fInputList = flag.Bool("input-list", false,
	"print available input plugins.")
var fOutputFilters = flag.String("output-filter", "",
	"filter the outputs to enable, separator is :")
var fOutputList = flag.Bool("output-list", false,
	"print available output plugins.")
var fAggregatorFilters = flag.String("aggregator-filter", "",
	"filter the aggregators to enable, separator is :")
var fProcessorFilters = flag.String("processor-filter", "",
	"filter the processors to enable, separator is :")
var fUsage = flag.String("usage", "",
	"print usage for a plugin, ie, 'telegraf -usage mysql'")
var fService = flag.String("service", "",
	"operate on the service")
var fSignal = flag.String("s", "", `avalible values are:
	stop - stops watchdog and agent
	reload - reload agent config
	restart - resetart watchdog and agent`)

// Telegraf version, populated linker.
//   ie, -ldflags "-X main.version=`git describe --always --tags`"
var (
	version string
	commit  string
	branch  string
)

func init() {
	// If commit or branch are not set, make that clear.
	if commit == "" {
		commit = "unknown"
	}
	if branch == "" {
		branch = "unknown"
	}
}

const usage = `Telegraf, The plugin-driven server agent for collecting and reporting metrics.

Usage:

  telegraf [commands|flags]

The commands & flags are:

  config             print out full sample configuration to stdout
  version            print the version to stdout

  -aggregator-filter string
    	filter the aggregators to enable, separator is :
  -alsologtostderr
    	log to standard error as well as files
  -config string
    	configuration file to load
  -config-directory string
    	directory containing additional *.conf files
  -httptest.serve string
    	if non-empty, httptest.NewServer serves on this address and blocks
  -input-filter string
    	filter the inputs to enable, separator is :
  -input-list
    	print available input plugins.
  -log_backtrace_at value
    	when logging hits line file:N, emit a stack trace
  -log_dir string
    	If non-empty, write log files in this directory
  -logtostderr
    	log to standard error instead of files
  -output-filter string
    	filter the outputs to enable, separator is :
  -output-list
    	print available output plugins.
  -pidfile string
    	file to write our pid to
  -pprof-addr string
    	pprof address to listen on, not activate pprof if empty
  -processor-filter string
    	filter the processors to enable, separator is :
  -sample-config
    	print out full sample configuration
  -service string
    	operate on the service
  -stderrthreshold value
    	logs at or above this threshold go to stderr
  -test
    	gather metrics, print them out, and exit
  -usage string
    	print usage for a plugin, ie, 'telegraf -usage mysql'
  -v value
    	log level for V logs
  -version
    	display the version
  -vmodule value
    	comma-separated list of pattern=N settings for file-filtered logging
  -watch-Update
    	watch etcd for update config change, required watchdog to be true (default true)
  -watchdog
    	start watchdog to start process when exited (default true)


Examples:

  # generate a telegraf config file:
  telegraf config > telegraf.conf

  # generate config with only cpu input & influxdb output plugins defined
  telegraf --input-filter cpu --output-filter influxdb config

  # run a single telegraf collection, outputing metrics to stdout
  telegraf --config telegraf.conf -test

  # run telegraf with all plugins defined in config file
  telegraf --config telegraf.conf

  # run telegraf, enabling the cpu & memory input, and influxdb output plugins
  telegraf --config telegraf.conf --input-filter cpu:mem --output-filter influxdb

  # run telegraf with pprof
  telegraf --config telegraf.conf --pprof-addr localhost:6060
`

var stop chan struct{}

func reloadLoop(
	stop chan struct{},
	inputFilters []string,
	outputFilters []string,
	aggregatorFilters []string,
	processorFilters []string,
) {
	reload := make(chan bool, 1)
	reload <- true
	for <-reload {
		reload <- false

		// If no other options are specified, load the config file and run.
		c := config.NewConfig()
		c.OutputFilters = outputFilters
		c.InputFilters = inputFilters
		err := c.LoadConfig(*fConfig)
		if err != nil {
			glog.Fatal(err.Error())
		}

		if *fConfigDirectory != "" {
			err = c.LoadDirectory(*fConfigDirectory)
			if err != nil {
				glog.Fatal(err.Error())
			}
		}
		if !*fTest && len(c.Outputs) == 0 {
			glog.Fatal("Error: no outputs found, did you provide a valid config file?")
		}
		if len(c.Inputs) == 0 {
			glog.Fatal("Error: no inputs found, did you provide a valid config file?")
		}

		ag, err := agent.NewAgent(c)
		if err != nil {
			glog.Fatal(err.Error())
		}

		logger.InitLogs()

		if *fTest {
			err = ag.Test()
			if err != nil {
				glog.Fatal(err.Error())
			}
			os.Exit(0)
		}

		err = ag.Connect()
		if err != nil {
			glog.Fatal(err.Error())
		}

		shutdown := make(chan struct{})
		signals := make(chan os.Signal)
		signal.Notify(signals, os.Interrupt, syscall.SIGHUP, syscall.SIGTERM)
		go func() {
			select {
			case sig := <-signals:
				if sig == os.Interrupt || sig == syscall.SIGTERM {
					close(shutdown)
				}
				if sig == syscall.SIGHUP {
					glog.Infof("Reloading Telegraf config\n")
					<-reload
					reload <- true
					close(shutdown)
				}
			case <-stop:
				close(shutdown)
			}
		}()

		glog.Infof("Starting Telegraf (version %s)\n", version)
		glog.Infof("Loaded outputs: %s", strings.Join(c.OutputNames(), " "))
		glog.Infof("Loaded inputs: %s", strings.Join(c.InputNames(), " "))
		glog.Infof("Tags enabled: %s", c.ListTags())

		ag.Run(shutdown)
	}
}

func usageExit(rc int) {
	fmt.Println(usage)
	os.Exit(rc)
}

func main() {
	// flag.Usage = func() { usageExit(0) }
	flag.Parse()
	args := flag.Args()

	inputFilters, outputFilters := []string{}, []string{}
	if *fInputFilters != "" {
		inputFilters = strings.Split(":"+strings.TrimSpace(*fInputFilters)+":", ":")
	}
	if *fOutputFilters != "" {
		outputFilters = strings.Split(":"+strings.TrimSpace(*fOutputFilters)+":", ":")
	}

	aggregatorFilters, processorFilters := []string{}, []string{}
	if *fAggregatorFilters != "" {
		aggregatorFilters = strings.Split(":"+strings.TrimSpace(*fAggregatorFilters)+":", ":")
	}
	if *fProcessorFilters != "" {
		processorFilters = strings.Split(":"+strings.TrimSpace(*fProcessorFilters)+":", ":")
	}

	if len(args) > 0 {
		switch args[0] {
		case "version":
			fmt.Printf("Telegraf v%s (git: %s %s)\n", version, branch, commit)
			return
		case "config":
			config.PrintSampleConfig(
				inputFilters,
				outputFilters,
				aggregatorFilters,
				processorFilters,
			)
			return
		}
	}

	// switch for flags which just do something and exit immediately
	switch {
	case *fOutputList:
		fmt.Println("Available Output Plugins:")
		for k := range outputs.Outputs {
			fmt.Printf("  %s\n", k)
		}
		return
	case *fInputList:
		fmt.Println("Available Input Plugins:")
		for k := range inputs.Inputs {
			fmt.Printf("  %s\n", k)
		}
		return
	case *fVersion:
		fmt.Printf("Telegraf v%s (git: %s %s)\n", version, branch, commit)
		return
	case *fSampleConfig:
		config.PrintSampleConfig(
			inputFilters,
			outputFilters,
			aggregatorFilters,
			processorFilters,
		)
		return
	case *fUsage != "":
		err := config.PrintInputConfig(*fUsage)
		err2 := config.PrintOutputConfig(*fUsage)
		if err != nil && err2 != nil {
			glog.Fatalf("%s and %s", err, err2)
		}
		return
	case *fSignal != "":
		handCMD(*fSignal)
		return
	}

	// test config file
	// parse etcd, ctrlscript file path
	t := config.NewConfig()
	if err := t.LoadConfig(*fConfig); err != nil {
		glog.Fatalf("load config: %v", err)
	}

	// init etcd config
	cfgfile := config.GetEtcdConfigFile()
	cfg, err := etcd.NewEtcdConfig(cfgfile)
	if err != nil {
		err := fmt.Errorf("load etcd config err: %v", err)
		glog.Fatalf("%v", err)
	}
	generic.SetEtcdConfig(cfg)

	glog.V(10).Infof("args: %v", args)

	if app.Start() {
		parent()
	} else {
		child(inputFilters, outputFilters, aggregatorFilters, processorFilters)
	}
}

func parent() {
	runtime.GOMAXPROCS(1)

	if *fPidfile != "" {
		if err := writePid(*fPidfile); err != nil {
			glog.Errorf("write pid failed: %v", err)
			fmt.Printf("write pid file %v error: %v", *fPidfile, err)
		} else {
			defer func() {
				err := os.Remove(*fPidfile)
				if err != nil {
					glog.Errorf("Unable to remove pidfile: %s", err)
				}
			}()
		}
	}

	signals := make(chan os.Signal)
	signal.Notify(signals, os.Interrupt, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGQUIT)
	for {
		select {
		case <-app.Done():
			return
		case sig := <-signals:
			// restart agent
			if sig == syscall.SIGHUP {
				glog.Info("Restart child")
				app.Restart(true)
			}

			// stop watchdog and agent
			if sig == syscall.SIGTERM || sig == os.Interrupt {
				glog.Infof("receive term signal, stop child and exit")
				app.StopChildAndExit()
				return
			}

			// SIGQUIT restart watchdog and agent
			if sig == syscall.SIGQUIT {
				app.Restart(false)
				timer := time.NewTimer(5 * time.Second)
				for {
					select {
					case <-app.Done():
						return
					case <-timer.C:
						fmt.Fprintf(os.Stderr, "timeout waiting watchdog to exit")
						return
					}
				}
			}
		}
	}
}

func child(inputFilters, outputFilters, aggregatorFilters, processorFilters []string) {
	// use at most 4 cpu
	numCPUs := hostinfo.GetNumOfCPUs()
	cpuUse := numCPUs / 4
	if cpuUse < 0 {
		cpuUse = 1
	} else if cpuUse > 4 {
		cpuUse = 4
	}
	runtime.GOMAXPROCS(cpuUse)
	if *pprofAddr != "" {
		go func() {
			pprofHostPort := *pprofAddr
			parts := strings.Split(pprofHostPort, ":")
			if len(parts) == 2 && parts[0] == "" {
				pprofHostPort = fmt.Sprintf("localhost:%s", parts[1])
			}
			pprofHostPort = "http://" + pprofHostPort + "/debug/pprof"

			glog.Infof("Starting pprof HTTP server at: %s", pprofHostPort)

			if err := http.ListenAndServe(*pprofAddr, nil); err != nil {
				glog.Fatal(err.Error())
			}
		}()
	}
	if runtime.GOOS == "windows" {
		log.Fatal("windows is not supported")
	} else {
		stop = make(chan struct{})
		reloadLoop(
			stop,
			inputFilters,
			outputFilters,
			aggregatorFilters,
			processorFilters,
		)
	}
}

func readPid(pidfile string) (int, error) {
	defPid := -65536
	if pidfile == "" {
		return defPid, errors.New("pidfile is empty")
	}

	d, err := ioutil.ReadFile(pidfile)
	if err != nil {
		return defPid, err
	}

	content := strings.TrimSpace(string(d))
	pid, err := strconv.Atoi(content)
	if err != nil {
		err = errors.Wrap(err, "read convert contend to pid")
		return defPid, err
	}
	return pid, nil
}

func writePid(pidfile string) error {
	pid := os.Getpid()
	return ioutil.WriteFile(pidfile, []byte(strconv.Itoa(pid)), 0644)
}

/*
	stop - stops watchdog and agent
	reload - reload agent config
	restart - resetart watchdog and agent`)
*/
func handCMD(cmd string) {
	switch cmd {
	case "stop":
		pids, err := app.FindProcesses()
		if err != nil {
			fmt.Fprintf(os.Stderr, "get pid list :%v", err)
		}
		glog.V(4).Infof("find processes: %v", pids)
		procs := []*os.Process{}
		for _, pid := range pids {
			proc, err := os.FindProcess(pid)
			if err != nil {
				continue
			}
			if err := proc.Signal(syscall.SIGTERM); err != nil {
				glog.Warningf("signal proc %v: %v", pid, err)
			}
			procs = append(procs, proc)
		}

		tick := time.NewTicker(10 * time.Millisecond)
		timer := time.NewTimer(5 * time.Second)
		for {
			select {
			case <-tick.C:
				runnningprocs := []*os.Process{}
				for _, proc := range procs {
					err := proc.Signal(syscall.Signal(0))
					if err == nil {
						runnningprocs = append(runnningprocs, proc)
						continue
					}
				}
				procs = runnningprocs
				if len(procs) == 0 {
					return
				}
			case <-timer.C:
				for _, proc := range procs {
					proc.Signal(syscall.SIGKILL)
				}
				return
			}
		}

	case "reload":
		if *fPidfile == "" {
			fmt.Fprintf(os.Stderr, "pidfile is empty")
			return
		}
		pid, err := readPid(*fPidfile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "read pid file %v err: %v", *fPidfile, err)
			return
		}
		proc, err := os.FindProcess(pid)
		if err != nil {
			fmt.Fprintf(os.Stderr, "find process %v err: %v", pid, err)
			return
		}
		if err := proc.Signal(syscall.SIGHUP); err != nil {
			fmt.Fprintf(os.Stderr, "signal process %v err: %v", pid, err)
		}
		return

	case "restart":
		if *fPidfile == "" {
			fmt.Fprintf(os.Stderr, "pidfile is empty")
			return
		}
		pid, err := readPid(*fPidfile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "read pid file %v err: %v\n", *fPidfile, err)
			return
		}
		proc, err := os.FindProcess(pid)
		if err != nil {
			fmt.Fprintf(os.Stderr, "find process %v err: %v\n", pid, err)
			return
		}

		if err := proc.Signal(syscall.SIGQUIT); err != nil {
			fmt.Fprintf(os.Stderr, "signal process %v err: %v", pid, err)
		}

		done, err := isProcStopped(proc, 5*time.Second)
		if err != nil {
			fmt.Fprintf(os.Stderr, "wait watchdog exit: %v\n", err)
		}
		if !done {
			fmt.Fprintf(os.Stderr, "failed: force kill\n")
			proc.Kill()
		}

		pids, _ := app.FindProcesses()
		fmt.Printf("current running pids: %v\n", pids)
		return
	default:
		flag.Usage()
		return
	}
}

func isProcStopped(proc *os.Process, timeout time.Duration) (bool, error) {
	ticker := time.NewTicker(10 * time.Millisecond)
	timer := time.NewTicker(5 * time.Second)
	for {
		select {
		case <-ticker.C:
			err := proc.Signal(syscall.Signal(0))
			if err == nil {
				continue
			}
			errMsg := err.Error()
			if strings.Contains(errMsg, "no such process") || strings.Contains(errMsg, "process already finished") {
				return true, nil
			}
			return false, err
		case <-timer.C:
			return false, errors.New("timeout")
		}
	}

}
