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

// GetClusterDeployConfig return deploy config type cluster c
func (r *Registry) GetClusterDeployConfig(typ types.ProjectType, c types.UUID) (*types.DeployConfig, error) {
	if typ == types.EmptyProjectType || c == types.EmptyUUID {
		return nil, fmt.Errorf("both type and cluster cannot be empty, got: (%v, %v)", typ, c)
	}

	key := etcd.GetClusterDeploy(r.env, typ, c)

	store, err := getStore()
	if err != nil {
		return nil, err
	}

	ret := &types.DeployConfig{}
	err = store.Get(context.TODO(), key, ret, false)
	return ret, err
}

// SaveClusterDeployConfig saves deployconfig config, c should be a valid config, but is not checked here
func (r *Registry) SaveClusterDeployConfig(c *types.DeployConfig) error {
	if c.Type == types.EmptyProjectType || c.Cluster == types.EmptyUUID {
		return fmt.Errorf("both type and cluster cannot be empty, got: (%v, %v)", c.Type, c.Cluster)
	}

	key := etcd.GetClusterDeploy(r.env, c.Type, c.Cluster)

	store, err := getStore()
	if err != nil {
		return err
	}

	ret := &types.DeployConfig{}
	err = store.Update(context.TODO(), key, c, ret, 0)

	glog.V(10).Infof("udpate cluster deploy info, before: %v, after: %v", ret, c)

	return nil
}

// DeleteClusterDeployConfig  delete deploy config of cluster c
func (r *Registry) DeleteClusterDeployConfig(typ types.ProjectType, c types.UUID) error {
	if typ == types.EmptyProjectType || c == types.EmptyUUID {
		return fmt.Errorf("both type and cluster cannot be empty, got: (%v, %v)", typ, c)
	}

	key := etcd.GetClusterDeploy(r.env, typ, c)

	store, err := getStore()
	if err != nil {
		return err
	}
	ret := &types.DeployConfig{}
	err = store.Delete(context.TODO(), key, ret)
	glog.V(10).Infof("delete projectConfig: %v", ret)
	return err
}

// GetClusterDeployConfigOfType return a map of all deploy configs of a give type typ
func (r *Registry) GetClusterDeployConfigOfType(typ types.ProjectType) (map[types.UUID]*types.DeployConfig, error) {
	if typ == types.EmptyProjectType {
		return nil, fmt.Errorf("typecannot be empty, got: %v", typ)
	}

	key := etcd.GetClusterDeployPrefixOfType(r.env, typ)

	store, err := getStore()
	if err != nil {
		return nil, err
	}
	out := []*types.DeployConfig{}
	err = store.List(context.TODO(), key, generic.Everything, &out)
	if err != nil {
		glog.V(10).Infof("getclusterDeploy config type type err :%v", err)
		return nil, err
	}
	ret := make(map[types.UUID]*types.DeployConfig, len(out))
	for _, o := range out {
		ret[o.Cluster] = o
	}

	return ret, nil
}

// WatchDeployConfig for changes
func (r *Registry) WatchDeployConfig(handler watch.EventHandler) error {
	store, err := getStore()
	if err != nil {
		glog.Errorf("getStoreInstance error: %s", err)
		return err
	}

	key := etcd.GetClusterDeployConfigPrefix(r.env)
	watcher, err := store.Watch(context.Background(), key, generic.Everything, true, reflect.TypeOf(types.DeployConfig{}))
	if err != nil {
		return err
	}

	for {
		select {
		case event := <-watcher.ResultChan():
			glog.V(4).Infof("receiver deploy config event: %v", event)
			switch event.Type {
			case watch.Error:
				err, ok := event.Object.(error)
				if !ok {
					glog.Warningf("event type error, but event.Object is not an error")
					err = fmt.Errorf("watch got error :%v", event.Object)
				}
				glog.Warningf("watch err: %v", err)
			default:
				if err := handler(event); err != nil {
					glog.Fatalf("handle deploy config event: %v", err)
				}
			}
		case <-r.stopC:
			watcher.Stop()
			return nil
		}
	}
}
