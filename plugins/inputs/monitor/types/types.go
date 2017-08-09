package types

import (
	"io"
	"time"

	"we.com/jiabiao/common/probe"
	"we.com/jiabiao/monitor/core/types"
)

// The following types are used internally in problem detector. In the future this could be the
// interface between node problem detector and other problem daemons.
// We added these types because:
// 1) The kubernetes api packages are too heavy.
// 2) We want to make the interface independent with kubernetes api change.

// Severity is the severity of the problem event. Now we only have 2 severity levels: Info and Warn,
// which are corresponding to the current kubernetes event types. We may want to add more severity
// levels in the future.
type Severity string

const (
	// Info is translated to a normal event.
	Info Severity = "info"
	// Warn is translated to a warning event.
	Warn Severity = "warn"
	// Error is translated to a error event.
	Error Severity = "error"
)

// Condition is the node condition used internally by problem detector.
type Condition struct {
	// Type is the condition type. It should describe the condition of node in problem. For example
	// KernelDeadlock, OutOfResource etc.
	Type string `json:"type"`
	// Status indicates whether the node is in the condition or not.
	Status bool `json:"status"`
	// Transition is the time when the node transits to this condition.
	Transition time.Time `json:"transition"`
	// Reason is a short reason of why node goes into this condition.
	Reason string `json:"reason"`
	// Message is a human readable message of why node goes into this condition.
	Message string `json:"message"`
}

// Event is the event used internally by node problem detector.
type Event struct {
	// Severity is the severity level of the event.
	Severity Severity `json:"severity"`
	// Timestamp is the time when the event is generated.
	Timestamp time.Time `json:"timestamp"`
	// Reason is a short reason of why the event is generated.
	Reason string `json:"reason"`
	// Message is a human readable message of why the event is generated.
	Message string `json:"message"`
}

// Status is the status other problem daemons should report to node problem detector.
type Status struct {
	// Source is the name of the problem daemon.
	Source string `json:"source"`
	// Events are temporary node problem events. If the status is only a condition update,
	// this field could be nil. Notice that the events should be sorted from oldest to newest.
	Events []Event `json:"events"`
	// Conditions are the permanent node conditions. The problem daemon should always report the
	// newest node conditions in this field.
	Conditions []Condition `json:"conditions"`
}

// Metric is report point
type Metric struct {
	Name   string
	Tags   map[string]string
	Fields map[string]interface{}
	Time   time.Time
}

// ProcessInfor is a interface to get Process Info
type ProcessInfor interface {
	GetNodeID() string
	GetProcessMetric() (metric *Metric, err error)
	GetTypeSpecMetrics() (metric *Metric, err error)
	GetClusterNameAndVersion() (cluster types.UUID, version string)
	Probe() probe.Result

	GetMonitorType() types.MonitorType

	// value between 0 and 1,  larger value consumer more system resource
	GetResConsumerEvaluate() float64
}

// Monitor is implements of specific type  of monitor items
type Monitor interface {
	// GetType current monitor type:  java, nginx, es, mq,  redis,  zk etc.
	GetType() types.MonitorType

	// InitMonitor  init monitor
	InitMonitor(NodeEventPusher, ProcessWorker) error

	StopMonitor() error

	//GetProcessList return a list of running processes of this type
	GetProcessPidList() ([]ProcessInfor, error)

	// start a process from the given info
	StartProcess(tsps *types.ClusterReplicaSpec, o io.Writer) error

	// Stop stops a process of the pid
	StopProcess(p ProcessInfor, o io.Writer) error

	// Report instance info to etcd
	ReportInstances() error
	// CleanOldInstances will be call after InitMonitor
	CleanOldInstances() error
}

type NodeEventType string

const (
	NodeStopped NodeEventType = "stopped"
	NodeCreated NodeEventType = "created"
	NodeUpdated NodeEventType = "updated"
)

// NodeEvent  node status  changes
type NodeEvent struct {
	Type        NodeEventType
	ProcessInfo ProcessInfor
}

// NodeEventPusher push new event to manager
type NodeEventPusher interface {
	PushEvent(NodeEvent)
}

// Task  represent a job
type Task struct {
	CMD          string // cmd to execute
	Cluster      types.UUID
	Action       string
	MetricLabels map[string]string
	MetricFileds map[string]interface{}

	Deadline time.Time     // after which this task nolong need to execute, if iszero then ignored
	Timeout  time.Duration // timeout to execute this cmd
}

// ProcessWorker start | stop | restart a process
type ProcessWorker interface {
	AddTask(t Task)
}
