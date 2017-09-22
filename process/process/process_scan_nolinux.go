// +build !linux

package ps

import (
	"fmt"
	"regexp"
	"syscall"

	"github.com/pkg/errors"
)

// Typ patten type
type Typ string

const (
	pgrepPattern Typ = "pattern"
	pgrepExe     Typ = "exe"
	unknown          = "unknown"
)

// PidType  pidtype
type PidType struct {
	Typ  Typ
	Args string
}

// GetRegexp get re
func (pt *PidType) GetRegexp() (*regexp.Regexp, error) {
	return nil, errors.New("not supported")
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
	return nil, nil
}

// PKill implements pkill
func PKill(name string, sig syscall.Signal, matchBinOnly bool) error {
	return nil
}

// Pgrep implements pgrep command
func Pgrep(name string, matchBinOnly bool) ([]int, error) {

	return nil, nil
}

func matchCmdline(cmdline string, pt *PidType) bool {
	return false
}
