// +build linux

package process

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"unicode"

	"github.com/golang/glog"
	multierror "github.com/hashicorp/go-multierror"
)

// Typ patten type
type Typ string

const (
	pgrepPattern Typ = "pattern"
	pgrepExe     Typ = "exe"
)

// PidType  pidtype
type PidType struct {
	Typ  Typ
	Args string
}

var (
	projectTypeCfg = map[string]PidType{
		"java": PidType{
			Typ:  pgrepPattern,
			Args: "Djava.apps.prog",
		},
		"nginx": PidType{
			Typ:  pgrepPattern,
			Args: "nginx: master process",
		},

		"es": PidType{
			Typ:  pgrepPattern,
			Args: "org.elasticsearch.bootstrap.Elasticsearch",
		},
		"rabbitmq": PidType{
			Typ:  pgrepPattern,
			Args: "-rabbit plugins_expand_dir",
		},
		"redis": PidType{
			Typ:  pgrepExe,
			Args: "redis-server",
		},
	}
)

func pidsFromExe(exe string) ([]int, error) {
	exe = fmt.Sprintf("^[^\x00]*/?%s$", exe)
	return Pgrep(exe, true)
}

func pidsFromPattern(pattern string) ([]int, error) {
	return Pgrep(pattern, false)
}

// GetAllPidsOfType return all pids of type type
func GetAllPidsOfType(typ string) ([]int, error) {
	ntyp := strings.ToLower(typ)
	var pidtype PidType
	var ok bool
	switch ntyp {
	case "java":
		pidtype, ok = projectTypeCfg["java"]
	case "nginx":
		pidtype, ok = projectTypeCfg["nginx"]
	case "es", "elasticsearch":
		pidtype, ok = projectTypeCfg["es"]
	case "mq", "rabbitmq":
		pidtype, ok = projectTypeCfg["rabbitmq"]
	case "redis":
		pidtype, ok = projectTypeCfg["redis"]
	default:
		return nil, fmt.Errorf("unknown project type %v", typ)
	}

	if !ok {
		return nil, fmt.Errorf("unknown project type %v", typ)
	}

	switch pidtype.Typ {
	case pgrepPattern:
		return pidsFromPattern(pidtype.Args)
	case pgrepExe:
		return pidsFromExe(pidtype.Args)
	default:
		return nil, fmt.Errorf("unknown pidType %v, valid ones: %v, %v", pidtype.Typ, pgrepPattern, pgrepExe)
	}
}

func getPids(re *regexp.Regexp, matchBinOnly bool) []int {
	pids := []int{}
	filepath.Walk("/proc", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// We should continue processing other directories/files
			return nil
		}
		base := filepath.Base(path)
		// Traverse only the directories we are interested in
		if info.IsDir() && path != "/proc" {
			// If the directory is not a number (i.e. not a PID), skip it
			if _, err := strconv.Atoi(base); err != nil {
				return filepath.SkipDir
			}
		}
		if base != "cmdline" {
			return nil
		}
		cmdline, err := ioutil.ReadFile(path)
		if err != nil {
			glog.V(4).Infof("Error reading file %s: %+v", path, err)
			return nil
		}
		exe := []string{}
		if matchBinOnly {
			// The bytes we read have '\0' as a separator for the command line
			parts := bytes.SplitN(cmdline, []byte{0}, 2)
			if len(parts) == 0 {
				return nil
			}
			// Split the command line itself we are interested in just the first part
			exe = strings.FieldsFunc(string(parts[0]), func(c rune) bool {
				return unicode.IsSpace(c) || c == ':'
			})
		} else {
			exe = []string{string(cmdline)}
		}
		if len(exe) == 0 {
			return nil
		}
		// Check if the name of the executable is what we are looking for
		if re.MatchString(exe[0]) {
			dirname := filepath.Base(filepath.Dir(path))
			// Grab the PID from the directory path
			pid, _ := strconv.Atoi(dirname)
			pids = append(pids, pid)
		}
		return nil
	})
	return pids
}

// PKill implements pkill
func PKill(name string, sig syscall.Signal, matchBinOnly bool) error {
	if len(name) == 0 {
		return fmt.Errorf("name should not be empty")
	}
	re, err := regexp.Compile(name)
	if err != nil {
		return err
	}
	pids := getPids(re, matchBinOnly)
	if len(pids) == 0 {
		return fmt.Errorf("unable to fetch pids for process name : %q", name)
	}

	var merr *multierror.Error
	for _, pid := range pids {
		if err = syscall.Kill(pid, sig); err != nil {
			merr = multierror.Append(merr, err)
		}
	}
	return merr.ErrorOrNil()
}

// Pgrep implements pgrep command
func Pgrep(name string, matchBinOnly bool) ([]int, error) {

	re, err := regexp.Compile(name)
	if err != nil {
		return nil, err
	}
	return getPids(re, matchBinOnly), nil
}
