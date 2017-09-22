// +build linux

package ps

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

	multierror "github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"
)

// Typ patten type
type Typ string

const (
	pgrepPattern Typ = "pattern"
	pgrepExe     Typ = "exe"
	unknown       = "unknown"
)

// PidType  pidtype
type PidType struct {
	Typ  Typ
	Args string
	re   *regexp.Regexp
}

// GetRegexp get re
func (pt *PidType) GetRegexp() (*regexp.Regexp, error) {
	if pt.re != nil {
		return pt.re, nil
	}

	if len(pt.Args) == 0 {
		return nil, errors.New("args cannot be empty string")
	}

	exe :=   pt.Args
	if pt.Typ == pgrepExe {
		exe = fmt.Sprintf("^[^\x00]*/?%s$", pt.Args)
	}
	pt.re, err := regexp.Compile(exe)
	return pt.re, err
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

func getPids(pt *PidType) []int {
	proc := "/proc"
	skip := filepath.SkipDir

	pids := []int{}
	filepath.Walk(proc, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// We should continue processing other directories/files
			return skip
		}
		if path == proc {
			return nil
		}

		base := filepath.Base(path)
		var pid int
		// Traverse only the directories we are interested in
		if info.IsDir() {
			// If the directory is not a number (i.e. not a PID), skip it
			if pid, err = strconv.Atoi(base); err != nil {
				return skip
			}
		}

		file := filepath.Join(path, "cmdline")

		cmdline, err := ioutil.ReadFile(file)
		if err != nil {
			return skip
		}
		if matchCmdline(cmdline, pt) {
			pids = append(pids, pid)
		}
		return skip
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

	pt := &PidType{
		re: re,
		Typ: pgrepPattern
	}
	if matchBinOnly {
		pt.Typ = pgrepExe
	}

	pids := getPids(pt)
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

	pt := &PidType{
		re: re,
		Typ: pgrepPattern
	}
	if matchBinOnly {
		pt.Typ = pgrepExe
	}


	return getPids(pt), nil
}

func matchCmdline(cmdline string, pt *PidType) bool {
	exe := []string{}
	if pt.Typ ==  pgrepExe {
		// The bytes we read have '\0' as a separator for the command line
		parts := bytes.SplitN(cmdline, []byte{0}, 2)
		if len(parts) == 0 {
			return skip
		}
		// Split the command line itself we are interested in just the first part
		exe = strings.FieldsFunc(string(parts[0]), func(c rune) bool {
			return unicode.IsSpace(c) || c == ':'
		})
	} else {
		exe = []string{string(cmdline)}
	}
	if len(exe) == 0 {
		return false
	}

	re, _ := pt.GetRegexp()

	// Check if the name of the executable is what we are looking for
	return re.MatchString(exe[0])
}
