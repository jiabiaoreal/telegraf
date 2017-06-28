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

// SaveConfig save or overwrite host config
func (r *Registry) SaveConfig(hc *types.HostConfig) error {
	store, err := getStore()
	if err != nil {
		return err
	}

	key := etcd.GetHostConfigPath(r.env, types.UUID(hc.HostID))

	return store.Update(context.TODO(), key, hc, nil, 0)
}

// GetConfig get config of a hostID, if config not exist return not exist error
func (r *Registry) GetConfig(hostID types.UUID) (*types.HostConfig, error) {
	store, err := getStore()
	if err != nil {
		return nil, err
	}

	key := etcd.GetHostConfigPath(r.env, hostID)

	ret := types.HostConfig{}

	if err = store.Get(context.TODO(), key, &ret, false); err != nil {
		return nil, err
	}

	return &ret, nil
}

// WatchConfig get config of a hostID, if config not exist return not exist error
func (r *Registry) WatchConfig(ctx context.Context, hostID types.UUID,
	handler watch.EventHandler) error {
	store, err := getStore()
	if err != nil {
		return err
	}

	key := etcd.GetHostConfigPath(r.env, hostID)
	watcher, err := store.Watch(context.Background(), key, generic.Everything, true,
		reflect.TypeOf(types.HostConfig{}))
	if err != nil {
		return err
	}

	for {
		select {
		case event := <-watcher.ResultChan():
			glog.Infof("receiver hostconfig new event: %v, %v", event.Type, event.Key)
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
					glog.Fatalf("handle hostConfig event: %v", err)
				}
			}

		case <-r.stopC:
			watcher.Stop()
			return nil
		case <-ctx.Done():
			watcher.Stop()
			return nil
		}
	}
}
