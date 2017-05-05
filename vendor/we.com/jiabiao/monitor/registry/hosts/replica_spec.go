package hosts

import (
	"context"
	"fmt"
	"reflect"

	"github.com/golang/glog"
	"we.com/jiabiao/monitor/core/etcd"
	"we.com/jiabiao/monitor/core/types"
	"we.com/jiabiao/monitor/registry/generic"
	"we.com/jiabiao/monitor/registry/watch"
)

// WatchHostReplicaSpec watch replicatSpec change for hostID
func (r *Registry) WatchHostReplicaSpec(hostID types.UUID, handler watch.EventHandler) error {
	store, err := getStore()
	if err != nil {
		glog.Errorf("getStoreInstance error: %s", err)
		return err
	}

	key := etcd.GetHostReplicaSpecPath(r.env, hostID)

	watcher, err := store.Watch(context.Background(), key, generic.Everything, false, reflect.TypeOf(types.HostReplicaSpec{}))
	if err != nil {
		return err
	}

	for {
		select {
		case event := <-watcher.ResultChan():
			glog.V(4).Infof("receiver new event: %v", event)
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
					glog.Fatalf("handle host replica spec event: %v", err)
				}
			}
		case <-r.stopC:
			watcher.Stop()
			return nil
		}
	}
}

// WatchHostReplicaSpecs watch replicatSpec change for hostID
func (r *Registry) WatchHostReplicaSpecs(handler watch.EventHandler) error {
	store, err := getStore()
	if err != nil {
		glog.Errorf("getStoreInstance error: %s", err)
		return err
	}

	key := etcd.GetHostReplicaSpecPrefix(r.env)
	watcher, err := store.Watch(context.Background(), key, generic.Everything, true, reflect.TypeOf(types.HostReplicaSpec{}))
	if err != nil {
		return err
	}

	for {
		select {
		case event := <-watcher.ResultChan():
			glog.Infof("receiver HostReplicaSpecs new event: %v, %v", event.Type, event.Key)
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
					glog.Fatalf("handle host replica spec event: %v", err)
				}
			}

		case <-r.stopC:
			watcher.Stop()
			return nil
		}
	}
}

// GetReplicaSpec  return host  replica config
func (r *Registry) GetReplicaSpec(hostID types.UUID) (*types.HostReplicaSpec, error) {
	key := etcd.GetHostReplicaSpecPath(r.env, hostID)

	store, err := getStore()
	if err != nil {
		return nil, err
	}

	out := &types.HostReplicaSpec{}
	err = store.Get(context.TODO(), key, out, false)

	return out, err
}

// SetReplicaSpec this function should only be called by the mangager, agent should not call this
// todo(nathon.ji): a better way is have two different register types
func (r *Registry) SetReplicaSpec(hostID types.UUID, hrs *types.HostReplicaSpec) error {
	key := etcd.GetHostReplicaSpecPath(r.env, hostID)
	store, err := getStore()
	if err != nil {
		return err
	}

	err = store.Update(context.TODO(), key, hrs, nil, 0)

	return err
}
