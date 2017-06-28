package types

import (
	"fmt"
	"time"

	log "github.com/golang/glog"
)

// ProjectType  same with MonitorType
type ProjectType string

// ENV  environments indicator
type ENV string

// UUID uuid(universal unique indetifior)
type UUID string

// InstanceRouteStatus whether instance  is services
type InstanceRouteStatus string

// ServiceType show the instance is running, a daemon, a service, a onetime script  etc.
type ServiceType string

const (
	// EmptyProjectType used to check if an project type is empty
	EmptyProjectType ProjectType = ""

	// EmptyUUID used to check if an UUID type is empty
	EmptyUUID UUID = ""

	// FieldSperator sperator  cluster parts
	FieldSperator = ":"
)

const (
	// ServiceUnknown unknows service type
	ServiceUnknown ServiceType = "unknown"
	// ServiceService  service type, which provide a service, others can use
	ServiceService ServiceType = "service"
	// ServiceJobs  es jobs
	ServiceJobs ServiceType = "task"
	// ServiceDaemon  long run daemon process
	ServiceDaemon ServiceType = "daemon"
	// ServiceScript  short run daemon  process
	ServiceScript ServiceType = "script"
)

var (
	knownProjectTypes = []ProjectType{
		ProjectType("java"),
	}
)

// GetKnownProjectTypes return registerd ProjectTypes
func GetKnownProjectTypes() []ProjectType {
	ret := []ProjectType{}
	copy(ret, knownProjectTypes)
	return ret
}

// ClusterInfo static cluster infos
type ClusterInfo struct {
	Type        ProjectType       `json:"type"`
	ClusterName UUID              `json:"clusterName"` //  uniqic within its project type
	Desc        string            `json:"desc"`
	Owner       string            `json:"owner"`
	Labels      map[string]string `json:"labels"`
	Annotation  map[string]string `json:"annotation"`
	ServiceType ServiceType       `json:"serviceType"`

	additional interface{}
}

// ValidateConfig validate if the  config info is valid
func (cfg ClusterInfo) ValidateConfig() error {

	if cfg.ClusterName == "" {
		return fmt.Errorf("project name is nil")
	}

	if len(cfg.Desc) < 10 {
		log.Warningf("project %s desc less then 10 chars", cfg.ClusterName)
	}

	return nil
}

// Addr listening addr
type Addr struct {
	IP   string
	Port int
}

// Instance common  instance info
type Instance struct {
	UUID        UUID                `json:"uuid,omitempty"`
	ProjecType  ProjectType         `json:"projectType,omitempty"`
	HostID      UUID                `json:"hostID,omitempty"`
	User        string              `json:"user,omitempty"`
	Host        string              `json:"host,omitempty"`
	IP          string              `json:"ip,omitempty"`
	Listening   []Addr              `json:"listening,omitempyt"`
	Pid         int                 `json:"pid,omitempty"`
	ClusterName UUID                `json:"clusterName,omitempty"`
	Env         ENV                 `json:"env,omitempty"`
	Node        string              `json:"node,omitempty"`
	Version     string              `json:"version,omitempty"`
	StartTime   time.Time           `json:"startTime,omitempty"`
	StopTime    time.Time           `json:"stopTime,omitempty"`
	ServiceType ServiceType         `json:"seriviceType,omitempty"`
	UpdateTime  time.Time           `json:"updateTime,omitempty"`
	Status      InstanceStatus      `json:"status,omitempty"`
	LifeCycle   InstanceLifeCycle   `json:"lifeCycle,omitempty"`
	ErrMsg      string              `json:"errMsg,omitempty"`
	RouteStatus InstanceRouteStatus `json:"routeStatus,omitempty"`
	ResUsage    *InstanceResUsage   `json:"resUsage,omitempty"`

	Private interface{} `json:"provite,omitempyt,omitempty"`
}

// InstanceResUsage instance resource usage
type InstanceResUsage struct {
	Memory         uint64  `json:"memory,omitempty"`
	CPUTotal       float64 `json:"cpuTotal,omitempty"`
	CPUPercent     float64 `json:"cpuPercent,omitempty"`
	Threads        int     `json:"threads,omitempty"`
	DiskBytesRead  uint64  `json:"diskBytesRead,omitempty"`
	DiskBytesWrite uint64  `json:"diskBytesWrite,omitempty"`
}

// GetClusterNameWithVersionInfo  return cluster name with version info
func (ins Instance) GetClusterNameWithVersionInfo() UUID {
	return UUID(fmt.Sprintf("%v%v%v", ins.ClusterName, FieldSperator, ins.Version))
}

// InstanceStatus instance prob status
type InstanceStatus string

const (
	// InstanceWarning  prob  status warning
	InstanceWarning InstanceStatus = "warning"
	// InstanceError prob error
	InstanceError InstanceStatus = "error"
	// InstanceSuccess  prob ok
	InstanceSuccess InstanceStatus = "success"
	// InstanceUnknown unknwon prob status
	InstanceUnknown InstanceStatus = "unknown"
)

// InstanceLifeCycle  life cycle of  instance
type InstanceLifeCycle string

const (
	// ILCStarting not ready for service
	ILCStarting InstanceLifeCycle = "starting"
	// ILCRunning  instance is up
	ILCRunning InstanceLifeCycle = "running"
	// ILCStopping instance is schedualed stopping or is stopping
	ILCStopping InstanceLifeCycle = "stopping"
	// ILCStopped instance already stopped
	ILCStopped InstanceLifeCycle = "stopped"
)
