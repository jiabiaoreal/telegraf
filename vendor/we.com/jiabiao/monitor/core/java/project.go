package java

import (
	"fmt"
	"io"
	"strings"
	"time"

	"we.com/jiabiao/common/yaml"
	"we.com/jiabiao/monitor/core/types"

	"github.com/golang/glog"
	multierror "github.com/hashicorp/go-multierror"
)

const (
	// Type java projectType
	Type types.ProjectType = "java"
	// MonitorType used for agent
	MonitorType types.MonitorType = "java"
	//APIv2  it2.0
	APIv2 string = "2.0"
	// APIv3 it3.0
	APIv3 string = "3.0"
	// APIv4 it4.0
	APIv4 string = "4.0"
)

// ProjectInterface a single interface confi
type ProjectInterface struct {
	Name        string            `json:"name"`
	Desc        string            `json:"desc"`
	Data        string            `json:"data"`
	Header      map[string]string `json:"header"`
	Env         string            `json:"env,omitempty"`
	Matches     map[string]string `json:"matches,omitempty"`
	DontMatches map[string]string `json:"dont_matches，omitempty"`
	EnvMap      map[types.ENV]int `json:"-"`
}

// Validate test if a projectInterface is valid
func (pi ProjectInterface) Validate() error {
	var err *multierror.Error

	if pi.Desc == "" {
		terr := fmt.Errorf("interface description cannot be nil")
		err = multierror.Append(err, terr)
	}

	if pi.Data == "" {
		terr := fmt.Errorf("interface data cannot be nil")
		err = multierror.Append(err, terr)
	}

	return err.ErrorOrNil()
}

// BinInfo project bins info
type BinInfo struct {
	Project string            `json:"project,omitempty"`
	Bin     string            `json:"name"`
	Type    types.ServiceType `json:"type"`
	Env     string            `json:"env,omitempty"`
	ZKPath  string            `json:"zkPath"`
	EnvMap  map[types.ENV]int `json:"-"`
}

// ProjectInfo  Project info
type ProjectInfo struct {
	APIVersion string                       `json:"apiVersion"`
	Name       string                       `json:"project,omitempty"`
	Desc       string                       `json:"desc,omitempty"`
	Owner      string                       `json:"owner"`
	Labels     map[string]string            `json:"labels"`
	ZKPath     string                       `json:"zkPath,omitempty"`
	Bins       map[string]*BinInfo          `json:"bins"`
	Interfaces map[string]*ProjectInterface `json:"interfaces"`
}

// GetDialInterfaces return dial interface of an env
func (pi *ProjectInfo) GetDialInterfaces(env types.ENV) map[string]*ProjectInterface {
	if pi == nil {
		return nil
	}

	if env == "" {
		return nil
	}

	retval := map[string]*ProjectInterface{}
	for k, v := range pi.Interfaces {
		if v.Env != "" {
			if _, ok := v.EnvMap[env]; ok {
				retval[k] = v
			}

		} else {
			retval[k] = v
		}
	}

	return retval
}

// Normalize project config
func (pi *ProjectInfo) Normalize() error {
	if err := pi.ValidateConfig(); err != nil {
		return err
	}

	for k, b := range pi.Bins {
		if b.Project == "" {
			b.Project = pi.Name
		}

		if b.Bin == "" {
			b.Bin = k
		}

		if b.Env != "" {
			b.EnvMap = map[types.ENV]int{}
			envs := strings.Split(b.Env, ",")
			for _, e := range envs {
				b.EnvMap[types.ENV(e)] = 1
			}
		}
	}

	for _, i := range pi.Interfaces {
		if i.Env != "" {
			i.EnvMap = map[types.ENV]int{}
			envs := strings.Split(i.Env, ",")
			for _, e := range envs {
				i.EnvMap[types.ENV(e)] = 1
			}
		}
		if len(i.Matches) == 0 && len(i.DontMatches) == 0 {
			i.Matches = map[string]string{
				"_contains": "success",
			}
		}
	}

	return nil

}

// ValidateConfig validate if config is valide
func (pi ProjectInfo) ValidateConfig() error {
	if pi.APIVersion == "" {
		return fmt.Errorf("APIVersion cannot be empty")
	}

	if pi.Name == "" {
		return fmt.Errorf("project name is nil")
	}

	if len(pi.Desc) < 10 {
		glog.Warningf("project %s desc less then 10 chars", pi.Name)
	}

	if pi.ZKPath == "" {
		return fmt.Errorf("project service name is empty")
	}
	if len(pi.Bins) == 0 {
		return fmt.Errorf("no bins configed")
	}

	if len(pi.Interfaces) < 3 {
		return fmt.Errorf("project dial interfaces  at least has three")
	}

	for _, it := range pi.Interfaces {
		if err := it.Validate(); err != nil {
			return err
		}
	}

	return nil
}

// ServiceNode zk node info
type ServiceNode struct {
	Host        string    `json:"address"`
	Type        int       `json:"type"`
	Port        int       `json:"port"`
	StartTime   time.Time `json:"startTime"`
	MainClass   string    `json:"mainclass"`
	Pid         int       `json:"pid"`
	ReconnectZK int       `json:"reconnectZK"`
	Version     string    `json:"version"`
	Methods     []string  `json:"method"`
}

// ToClusterInfo convert to a map of common cluserInfo
// clusterName ignored version num
func (pi *ProjectInfo) ToClusterInfo() map[types.UUID]*types.ClusterInfo {

	ret := map[types.UUID]*types.ClusterInfo{}
	for idstr, binfo := range pi.Bins {
		clusterStr := fmt.Sprintf("%v:%v:%v:%v:", pi.Name, types.FieldSperator, idstr, types.FieldSperator)
		id := types.UUID(clusterStr)
		ci := types.ClusterInfo{
			Type:        Type,
			ClusterName: id,
			Owner:       pi.Owner,
			Labels:      pi.Labels,
			ServiceType: binfo.Type,
		}
		ret[id] = &ci
	}

	return ret
}

// LoadProjectInfo  load projectInfo from reader
func LoadProjectInfo(reader io.Reader) (*ProjectInfo, error) {
	decoder := yaml.NewYAMLOrJSONDecoder(reader, 4)
	ret := ProjectInfo{}

	err := decoder.Decode(&ret)
	if err != nil {
		return nil, err
	}
	return &ret, nil
}

// Instance extra info for java instance
type Instance struct {
	Node types.UUID `json:"node"`
}

// ParseProjectBinInfo parse  project, bin from cluster name
func ParseProjectBinInfo(cluster types.UUID) (proj, bin string, err error) {
	parts := strings.Split(string(cluster), types.FieldSperator)
	if len(parts) == 2 {
		proj, bin = parts[0], parts[1]
		return
	}
	err = fmt.Errorf("parse java cluster %v for project and bin err", cluster)
	glog.Errorf("%v", err)
	return
}
