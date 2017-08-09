package types

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	// CPUUnit cpu hz
	CPUUnit = uint64(2.6 * 1000 * 1000 * 1000)
)

// DeployConfig config  how an project should be deployed
type DeployConfig struct {
	Type          ProjectType `json:"projectType"`
	Cluster       UUID        `json:"cluster"` // cluster unique defines an project of type Type
	NumOfInstance int         `json:"numOfInstance"`
	ServiceType   ServiceType `json:"serviceType"`

	SourceRepo      string `json:"sourceRepo,omitempty"`
	ReleaseGitRepo  string `json:"relaseRepo"`
	DeployGitBranch string `json:"deployBranch"`
	SourceDir       string `json:"sourceDir"`
	DeployDir       string `json:"deployDir,omitempty"`

	// these fields used to select which hosts can start this project
	Labels           map[string]string `json:"selector"`
	ResourceRequired DeployResource    `json:"resourceRequired"`
}

// DeployResource resource required to deploy an instance
type DeployResource struct {
	Memory           uint64 `json:"memory,omitempty"`
	CPU              uint64 `json:"cpu,omitempty"`
	NetworkIn        uint64 `json:"networkIn,omitempty"`
	NetworkOut       uint64 `json:"networkOut,omitempty"`
	DiskSpace        uint64 `json:"diskSpace,omitempty"`
	MaxAllowedMemory uint64 `json:"maxAllowedMemory,omitempty"`
	MaxAllowedCPU    uint64 `json:"maxAllowedCPU,omitempty"`
	MaxAllowdThreads int    `json:"maxAllowdThreads,omitempty"`
}

// Add add  operand resource usage to dr
func (dr *DeployResource) Add(operand DeployResource) {
	dr.Memory += operand.Memory
	dr.CPU += operand.CPU
	dr.NetworkIn += operand.NetworkIn
	dr.NetworkOut += operand.NetworkOut
	dr.DiskSpace += operand.DiskSpace
}

// Subtract ubtract  operand resource usage to dr
func (dr *DeployResource) Subtract(operand DeployResource) {
	if dr.Memory > operand.Memory {
		dr.Memory -= operand.Memory
	} else {
		dr.Memory = 0
	}

	if dr.CPU > operand.CPU {
		dr.CPU -= operand.CPU
	} else {
		dr.CPU = 0
	}

	if dr.NetworkIn > operand.NetworkIn {
		dr.NetworkIn -= operand.NetworkIn
	} else {
		dr.NetworkIn = 0
	}

	if dr.NetworkOut > operand.NetworkOut {
		dr.NetworkOut -= operand.NetworkOut
	} else {
		dr.NetworkOut = 0
	}
	if dr.DiskSpace > operand.DiskSpace {
		dr.DiskSpace -= operand.DiskSpace
	} else {
		dr.DiskSpace = 0
	}

}

// Devide devide  operand resource usage to dr
func (dr *DeployResource) Devide(operand DeployResource) int64 {
	min := operand.Memory
	if operand.Memory != 0 {
		t := dr.Memory / operand.Memory
		if t < min {
			min = t
		}
	}

	if operand.CPU != 0 {
		t := dr.CPU / operand.CPU
		if t < min {
			min = t
		}
	}

	if operand.DiskSpace != 0 {
		t := dr.DiskSpace / operand.DiskSpace
		if t < min {
			min = t
		}
	}

	if operand.NetworkIn != 0 {
		t := dr.NetworkIn / operand.NetworkIn
		if t < min {
			min = t
		}
	}
	if operand.NetworkOut != 0 {
		t := dr.NetworkOut / operand.NetworkOut
		if t < min {
			min = t
		}
	}

	return int64(min)
}

// UnmarshalJSON implements json marshal interface
func (dr *DeployResource) UnmarshalJSON(data []byte) error {
	type tmp struct {
		Memory     resUnit `json:"memory,omitempty"`
		CPU        resUnit `json:"cpu,omitempty"`
		NetworkIn  resUnit `json:"networkIn,omitempty"`
		NetworkOut resUnit `json:"networkOut,omitempty"`
		DiskSpace  resUnit `json:"diskSpace,omitempty"`
	}

	t := tmp{}

	if err := json.Unmarshal(data, &t); err != nil {
		return err
	}

	dr.CPU = uint64(t.CPU)
	dr.Memory = uint64(t.Memory)
	dr.NetworkIn = uint64(t.NetworkIn)
	dr.NetworkOut = uint64(t.NetworkOut)
	dr.DiskSpace = uint64(t.DiskSpace)

	return nil
}

func (dr DeployResource) String() string {
	d, _ := json.Marshal(dr)

	return string(d)
}

// MarshalJSON implements json marshal interface
func (dr DeployResource) MarshalJSON() ([]byte, error) {
	type tmp struct {
		Memory     resUnit `json:"memory,omitempty"`
		CPU        resUnit `json:"cpu,omitempty"`
		NetworkIn  resUnit `json:"networkIn,omitempty"`
		NetworkOut resUnit `json:"networkOut,omitempty"`
		DiskSpace  resUnit `json:"diskSpace,omitempty"`
	}

	t := tmp{
		Memory:     resUnit(dr.Memory),
		CPU:        resUnit(dr.CPU),
		NetworkIn:  resUnit(dr.NetworkIn),
		NetworkOut: resUnit(dr.NetworkOut),
		DiskSpace:  resUnit(dr.DiskSpace),
	}

	return json.Marshal(t)
}

var (
	resourseRE = regexp.MustCompile(`^(\d+\.?\d*)((k|K|m|M|g|G)(b|B)?)?$`)
)

type resUnit uint64

func (ru *resUnit) UnmarshalJSON(data []byte) error {
	s := string(data)
	s = strings.Trim(s, `"`)

	d, err := ParseResoureValue(s)
	if err != nil {
		return err
	}

	*ru = resUnit(d)
	return nil
}

func (ru resUnit) String() string {
	unit := ""
	dat := uint64(ru)

	factors := map[string]uint64{
		"G": 1024 * 1024 * 1024,
		"M": 1024 * 1024,
		"K": 1024,
		"":  1,
	}

	tmp := dat
	for _, u := range []string{"K", "M", "G"} {
		if tmp/1024 > 0 {
			unit = u
			tmp /= 1024
		} else {
			break
		}
	}

	num := float64(dat)/float64(factors[unit]) + 0.5
	return fmt.Sprintf("%d%v", int64(num), unit)
}

func (ru resUnit) MarshalJSON() ([]byte, error) {
	return []byte(`"` + ru.String() + `"`), nil
}

// ParseResoureValue parse resource unit
func ParseResoureValue(res string) (uint64, error) {
	matches := resourseRE.FindStringSubmatch(res)
	if len(matches) != 5 {
		return 0, fmt.Errorf("not a valid resource unit string: %q", res)
	}

	n, _ := strconv.ParseFloat(matches[1], 64)

	switch unit := matches[3]; unit {
	case "k":
		n *= 1000
	case "K":
		n *= 1024
	case "m":
		n *= 1000 * 1000
	case "M":
		n *= 1024 * 1024
	case "g":
		n *= 1000 * 1000 * 1000
	case "G":
		n *= 1024 * 1024 * 1024
	case "":
		// Value already correct
	default:
		return 0, fmt.Errorf("unknown unit %v", res)
	}
	return uint64(n), nil
}

// ToClusterSpec  convert a  ClusterReplicaSpec, ignored version
// version info should be managered by  replicat controller
func (dc *DeployConfig) ToClusterSpec() *ClusterReplicaSpec {
	ret := ClusterReplicaSpec{
		Type: dc.Type,

		ClusterName:  dc.Cluster,
		InstancesNum: dc.NumOfInstance,
	}

	return &ret
}

// ToHostReplicaSpec convert a map of map to HostReplicaSpec
func ToHostReplicaSpec(ptc map[ProjectType]map[UUID]DeployConfig) *HostReplicaSpec {
	ret := HostReplicaSpec{}
	for _, tcMap := range ptc {
		for _, cs := range tcMap {
			ret.AddCluserSpec(*cs.ToClusterSpec())
		}
	}

	return &ret
}

// ToTypeReplicaSpec convert a map of map to HostReplicaSpec
func ToTypeReplicaSpec(tdc map[UUID]DeployConfig) *TypeReplicaSpec {
	ret := TypeReplicaSpec{}
	for _, cs := range tdc {
		ret.Add(*cs.ToClusterSpec())
	}

	return &ret
}
