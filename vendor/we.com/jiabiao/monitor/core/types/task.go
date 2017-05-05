package types

import "time"

// Task is an schedualed  job
type Task struct {
	ID        UUID
	StartTime time.Time
	Timeout   time.Duration
	Deadline  time.Time // after which this task nolong need to execute, if iszero then ignored
	Host      UUID

	CMD          string                 // cmd to execute
	MetricLabels map[string]string      // labels to report
	MetricFileds map[string]interface{} // fields to report
}

// TaskResult task execute result
type TaskResult struct {
	TaskID  UUID
	Status  int    //  task execute status 0 for success
	Message string // status execute out
	Errmsg  string // error message if failed
}
