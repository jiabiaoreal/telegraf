package instances

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/golang/glog"

	"we.com/jiabiao/monitor/core/etcd"
	"we.com/jiabiao/monitor/core/types"
	"we.com/jiabiao/monitor/registry/generic"
	"we.com/jiabiao/monitor/registry/watch"
)

// GetVersionInfo return versionInfo of typ cluster, if not found return nil, nil
// or nil, error if some thing bad happends, otherwise returns versioninfo, nil
func (r *Registry) GetVersionInfo(typ types.ProjectType, cluster types.UUID) (*types.VersionInfo, error) {
	store, err := getStore()
	if err != nil {
		return nil, err
	}

	key := etcd.GetClusterVersionInfoPrefixOfCluster(r.env, typ, cluster)

	ret := types.VersionInfo{}
	if err = store.Get(context.Background(), key, &ret, false); err != nil {
		if generic.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	return &ret, nil
}

// SaveVersionInfo save versioninfo to store, error will be returned, if vi has no typ, clustername,
// or other store relatied errors
func (r *Registry) SaveVersionInfo(vi *types.VersionInfo) error {
	store, err := getStore()
	if err != nil {
		return err
	}

	if vi.Type == types.EmptyProjectType || vi.Cluster == types.EmptyUUID {
		return errors.New("project type and cluster name cannot be nil")
	}

	key := etcd.GetClusterVersionInfoPrefixOfCluster(r.env, vi.Type, vi.Cluster)

	return store.Update(context.Background(), key, vi, nil, 0)
}

// ListVersionInfoOfType return verioninfo of typ, or part of the result and err if same bad thing happens
// key of map is clustername
func (r *Registry) ListVersionInfoOfType(typ types.ProjectType) (map[types.UUID]*types.VersionInfo, error) {
	store, err := getStore()
	if err != nil {
		return nil, err
	}

	key := etcd.GetClusterVersionInfoPrefixOfType(r.env, typ)

	out := []*types.VersionInfo{}

	err = store.List(context.Background(), key, generic.Everything, &out)

	ret := map[types.UUID]*types.VersionInfo{}
	for _, o := range out {
		ret[o.Cluster] = o
	}

	return ret, err
}

// WatchVersion watch version info changes
func (r *Registry) WatchVersion(ctx context.Context, handler watch.EventHandler) error {
	if handler == nil {
		return errors.New("handler is nil")
	}

	if ctx == nil {
		return errors.New("context cannot be nil")
	}

	store, err := getStore()
	if err != nil {
		glog.Errorf("getStoreInstance error: %s", err)
		return err
	}

	key := etcd.GetClusterVersionInfoPrefix()

	watcher, err := store.Watch(ctx, key, generic.Everything, true, reflect.TypeOf(types.VersionInfo{}))
	if err != nil {
		return err
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
				glog.Warningf("watch version info: %v", err)
			default:
				if err := handler(event); err != nil {
					glog.Fatalf("handle version info: %v", err)
				}
			}
		case <-r.stopC:
			watcher.Stop()
			return nil
		}
	}
}
