package types

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
)

// RestartType when to restart
type RestartType string

// RestartPolicy action taken when  process exits
type RestartPolicy struct {
	Type  RestartType `json:"type,omitempty"`
	Until time.Time   `json:"value,omitempty"`
}

// MarshalJSON json.Marshal interface
func (rp RestartPolicy) MarshalJSON() ([]byte, error) {
	if rp.Type == OneTime || rp.Type == Always {
		return json.Marshal(rp.Type)
	}

	type p RestartPolicy

	return json.Marshal(p(rp))
}

// UnmarshalJSON  json.Unmarshal  interface
func (rp *RestartPolicy) UnmarshalJSON(data []byte) error {
	var s RestartType
	err := json.Unmarshal(data, &s)

	if err == nil {
		if s != OneTime && s != Always {
			return errors.Errorf("unknown restart policy: %v", s)
		}

		rp.Type = s
		return nil
	}

	type p struct {
		Type  RestartType `json:"type,omitempty"`
		Value interface{} `json:"value,omitempty"`
	}

	t := p{}
	if err := json.Unmarshal(data, &t); err != nil {
		return err
	}

	if t.Type != Until {
		return errors.Errorf("unknown restart policy: %v", t.Type)
	}

	switch a := t.Value.(type) {
	case string:
		var format string
		switch len(a) {
		case 5:
			format = "15:04"
		case 10:
			format = "2006-01-02"
		case 16:
			format = "2006-01-02 15:04"
		case len(time.RFC3339):
			format = time.RFC3339
		default:
			return errors.Errorf("unknown date format for restart type %v, %v", t.Type, a)
		}

		d, err := time.Parse(format, a)
		if err != nil {
			return err
		}

		rp.Type = t.Type
		rp.Until = d
		return nil

	case float64:
		rp.Until = time.Unix(int64(a), 0)
		rp.Type = t.Type
	default:
		return errors.Errorf("unknown value for restart policy of %v, %v", t.Type, a)
	}

	return nil
}

var (
	// CPUUnit cpu hz
	CPUUnit = uint64(2.6 * 1000 * 1000 * 1000)

	imageName = regexp.MustCompile(`^[a-z]+([.-][a-z]+)*/[a-z]+([.-][a-z]+)*$`)

	// OneTime 执行一次,  程序退出后(正常或异常)， 什么都不干
	OneTime RestartType = "onetime"
	// Always 总是重启， 程序退出后自动拉起
	Always RestartType = "always"
	// Until 在一个时间之前， 总是重启， 之后停掉
	// 对于长时间运行的程序， 在停掉前会先告警
	Until RestartType = "until"
)

type DeployUpdatePolicy string

const (
	// Inplace default for java applications
	Inplace DeployUpdatePolicy = "inplace"
	// ABWorld default for php or web applicatsion
	ABWorld DeployUpdatePolicy = "abworld"
	// Versioned every version has its own folder
	Versioned DeployUpdatePolicy = "versioned"
)

// DeployConfig config  how an project should be deployed
type DeployConfig struct {
	Type          ProjectType `json:"projectType,omitempty"`
	Cluster       UUID        `json:"cluster,omitempty"` // cluster unique defines an project of type Type
	NumOfInstance int         `json:"numOfInstance,omitempty"`
	ServiceType   ServiceType `json:"serviceType,omitempty"`

	Image        string                 `json:"image,omitempty"`
	DeployDir    string                 `json:"deployDir,omitempty"`
	Values       map[string]interface{} `json:"values,omitempty"`
	UpdatePolicy DeployUpdatePolicy     `json:"updatePolicy,omitempty"`

	// these fields used to select which hosts can start this project
	Labels           map[string]string `json:"selector,omitempty"`
	ResourceRequired DeployResource    `json:"resourceRequired,omitempty"`

	// RestartPolicy action taken, when process exits,
	// default always, restat
	RestartPolicy RestartPolicy `json:"restartPolicy,omitempty"`
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

		ClusterName:   dc.Cluster,
		InstancesNum:  dc.NumOfInstance,
		RestartPolicy: dc.RestartPolicy,
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
