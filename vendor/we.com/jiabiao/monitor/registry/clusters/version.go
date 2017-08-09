package clusters

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

// GetVersionInfo return version info of cluster
func (r *Registry) GetVersionInfo(typ types.ProjectType, cluster types.UUID) (*types.VersionInfo, error) {
	if typ == types.EmptyProjectType || cluster == types.EmptyUUID {
		return nil, fmt.Errorf("both type and cluster cannot be empty, got: (%v, %v)", typ, cluster)
	}

	key := etcd.GetClusterVersionInfo(r.env, typ, cluster)

	store, err := getStore()
	if err != nil {
		return nil, err
	}

	ret := &types.VersionInfo{}
	err = store.Get(context.TODO(), key, ret, false)
	return ret, err
}

// GetVersionInfoOfType return version info of cluster
func (r *Registry) GetVersionInfoOfType(typ types.ProjectType) (map[types.UUID]*types.VersionInfo, error) {
	if typ == types.EmptyProjectType {
		return nil, fmt.Errorf("typecannot be empty, got: %v", typ)
	}

	key := etcd.GetClusterVersionInfoPrefixOfType(r.env, typ)

	store, err := getStore()
	if err != nil {
		return nil, err
	}

	out := []*types.VersionInfo{}
	err = store.List(context.TODO(), key, generic.Everything, &out)
	if err != nil {
		glog.V(10).Infof("getcluster version info of type %v err :%v", typ, err)
		return nil, err
	}
	ret := make(map[types.UUID]*types.VersionInfo, len(out))
	for _, o := range out {
		ret[o.Cluster] = o
	}

	return ret, nil
}

// SaveVersionInfo return version info of cluster
func (r *Registry) SaveVersionInfo(vi *types.VersionInfo) error {
	if vi.Cluster == types.EmptyUUID || vi.Type == types.EmptyProjectType {
		return fmt.Errorf("both type and cluster cannot be empty, got: (%v, %v)", vi.Type, vi.Cluster)
	}

	key := etcd.GetClusterVersionInfo(r.env, vi.Type, vi.Cluster)

	store, err := getStore()
	if err != nil {
		return err
	}

	err = store.Update(context.TODO(), key, vi, nil, 0)

	return nil
}

// WatchVersionInfo  watch version info for changes
func (r *Registry) WatchVersionInfo(handler watch.EventHandler) error {
	store, err := getStore()
	if err != nil {
		glog.Errorf("getStoreInstance error: %s", err)
		return err
	}

	key := etcd.GetClusterVersionInfoPrefixOfEnv(r.env)

	watcher, err := store.Watch(context.Background(), key, generic.Everything, true, reflect.TypeOf(types.VersionInfo{}))
	if err != nil {
		return err
	}

	for {
		select {
		case event := <-watcher.ResultChan():
			glog.V(4).Infof("receiver new version info event: %v", event)
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
					glog.Fatalf("handle version info event: %v", err)
				}
			}
		case <-r.stopC:
			watcher.Stop()
			return nil
		}
	}
}
