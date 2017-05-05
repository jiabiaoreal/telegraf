package monitor

import (
	"github.com/golang/glog"
	"github.com/influxdata/telegraf/plugins/inputs/monitor/types"
)

// EventPusher implements a types.EventPusher interface
type EventPusher struct {
}

var _ types.NodeEventPusher = &EventPusher{}

// PushEvent implements types.EventPusher interface
func (ep *EventPusher) PushEvent(event types.NodeEvent) {
	switch event.Type {

	case types.NodeStopped:
		glog.Warningf("node stopped: %v", event.ProcessInfo)

	case types.NodeUpdated, types.NodeCreated:
		glog.Infof("node statue updated: %v", event)
	default:
		glog.Errorf("noknow event type %v", event)
	}

}

// NewEventPusher return a new EventPusher
func NewEventPusher() *EventPusher {
	ep := EventPusher{}
	return &ep
}
