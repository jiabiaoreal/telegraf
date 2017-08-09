package instances

import (
	"context"
	"fmt"
	"reflect"

	"github.com/golang/glog"

	"we.com/jiabiao/common/fields"
	"we.com/jiabiao/common/labels"
	"we.com/jiabiao/monitor/core/etcd"
	"we.com/jiabiao/monitor/core/types"
	"we.com/jiabiao/monitor/registry/generic"
	"we.com/jiabiao/monitor/registry/watch"
)

// NewRegistry returns a Registry
func NewRegistry(env types.ENV) *Registry {
	return &Registry{
		env:   env,
		stopC: make(chan struct{}),
	}
}

func getStore() (generic.Interface, error) {
	return generic.GetStoreInstance(etcd.DeployBasePrefix, false)
}

// Registry  instance client
type Registry struct {
	env   types.ENV
	stopC chan struct{}
}

// GetHostActualReplicaSpec get actual recplica spec of hostid
func (r *Registry) GetHostActualReplicaSpec(hostid types.UUID) (*types.HostReplicaSpec, error) {
	pred := generic.SelectionPredicate{
		Label: labels.Everything(),
		Field: fields.ParseSelectorOrDie("hostID==" + string(hostid)),
		GetAttrs: func(obj interface{}) (labels.Set, fields.Set, error) {
			ins := obj.(**types.Instance)
			return nil, fields.Set{"hostID": (string)((**ins).HostID)}, nil
		},
	}

	return r.getReplicaSpec(pred)
}

// GetActualReplicaSpec get actual host replicat spec sum up all hosts
func (r *Registry) GetActualReplicaSpec() (*types.HostReplicaSpec, error) {
	pred := generic.Everything

	return r.getReplicaSpec(pred)
}

// GetReplicaSpecGroupByHost get host replicaspec group by host
func (r *Registry) GetReplicaSpecGroupByHost(pred generic.SelectionPredicate) (map[types.UUID]*types.HostReplicaSpec, error) {
	store, err := getStore()
	if err != nil {
		return nil, err
	}

	key := etcd.GetInstancePathPrefixOfEnv(r.env)

	out := []*types.Instance{}
	if err = store.List(context.TODO(), key, pred, &out); err != nil {
		glog.Errorf("err list instances: %v", err)
	}

	ret := map[types.UUID]*types.HostReplicaSpec{}
	for _, ins := range out {
		typ := ins.ProjecType
		value := types.ClusterReplicaSpec{
			Type:         typ,
			ClusterName:  ins.ClusterName,
			Version:      ins.Version,
			InstancesNum: 1,
		}

		spec, ok := ret[ins.HostID]
		if !ok {
			spec = &types.HostReplicaSpec{}
			ret[ins.HostID] = spec
		}
		spec.AddCluserSpec(value)
	}

	return ret, err
}

func (r *Registry) getReplicaSpec(pred generic.SelectionPredicate) (*types.HostReplicaSpec, error) {
	store, err := getStore()
	if err != nil {
		return nil, err
	}

	key := etcd.GetInstancePathPrefixOfEnv(r.env)

	out := []*types.Instance{}
	if err = store.List(context.TODO(), key, pred, &out); err != nil {
		glog.Errorf("err list instances: %v", err)
	}

	ret := types.HostReplicaSpec{}
	for _, ins := range out {
		typ := ins.ProjecType
		value := types.ClusterReplicaSpec{
			Type:         typ,
			ClusterName:  ins.ClusterName,
			Version:      ins.Version,
			InstancesNum: 1,
		}
		ret.AddCluserSpec(value)
	}

	return &ret, err

}

// WatchInstanceForStatChange watch ectd from status changes
func (r *Registry) WatchInstanceForStatChange(handler watch.EventHandler) error {
	store, err := getStore()
	if err != nil {
		glog.Errorf("getStoreInstance error: %s", err)
		return err
	}

	key := etcd.GetHostInfoPath(r.env, "")

	watcher, err := store.Watch(context.Background(), key, generic.Everything, true, reflect.TypeOf(types.HostInfo{}))
	if err != nil {
		return err
	}

	if handler == nil {
		handler = r.handelEvent
	}

	for {
		select {
		case event := <-watcher.ResultChan():
			glog.V(10).Infof("receive an instance event: %v", event)
			switch event.Type {
			case watch.Error:
				err, ok := event.Object.(error)
				if !ok {
					glog.Warningf("event type if error, but event.Object is not an error")
					err = fmt.Errorf("watch got error :%v", event.Object)
				}
				glog.Warningf("watch err: %v", err)
			default:
				if err := handler(event); err != nil {
					glog.Fatalf("handle instance event: %v", err)
				}
			}
		case <-r.stopC:
			watcher.Stop()
			return nil
		}

	}
}

// StopWatch stop watch
func (r *Registry) StopWatch() {
	close(r.stopC)
}

func (r *Registry) handelEvent(event watch.Event) error {
	glog.Infof("receive an event %v", event)
	return nil
}
