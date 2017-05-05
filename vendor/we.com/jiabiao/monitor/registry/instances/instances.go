package instances

import (
	"fmt"

	"github.com/golang/glog"
	"golang.org/x/net/context"
	"we.com/jiabiao/common/fields"
	"we.com/jiabiao/common/labels"
	"we.com/jiabiao/monitor/core/etcd"
	"we.com/jiabiao/monitor/core/types"
	"we.com/jiabiao/monitor/registry/generic"
)

// GetClusterInstances get instances of a give cluster
func (r *Registry) GetClusterInstances(typ types.ProjectType, cluster types.UUID) ([]*types.Instance, error) {
	if cluster == types.EmptyUUID || typ == types.EmptyProjectType {
		return nil, fmt.Errorf("typ and cluster cannot be empty, get (%v, %v)", typ, cluster)
	}

	key := etcd.GetInstancePathPrefixOfCluster(r.env, typ, cluster)

	store, err := getStore()
	if err != nil {
		return nil, err
	}

	ret := []*types.Instance{}

	err = store.List(context.TODO(), key, generic.Everything, &ret)

	return ret, err
}

// DelClusterInstance delete instance
func (r *Registry) DelClusterInstance(in *types.Instance) (*types.Instance, error) {
	if in.ProjecType == types.EmptyProjectType || in.UUID == types.EmptyUUID || in.ClusterName == types.EmptyUUID {
		return nil, fmt.Errorf("instance  type, cluster and uuid cannot be empty, get (%v, %v, %v)", in.ProjecType, in.ClusterName, in.UUID)
	}

	key := etcd.GetInstancePath(r.env, in.ProjecType, in.ClusterName, in.UUID)

	store, err := getStore()
	if err != nil {
		return nil, err
	}

	out := &types.Instance{}

	err = store.Delete(context.TODO(), key, out)
	glog.V(10).Infof("delete instance: %v", out)

	return out, err
}

// GetInstance get instane of uuid
func (r *Registry) GetInstance(typ types.ProjectType, cluster types.UUID, id types.UUID) (*types.Instance, error) {
	if typ == types.EmptyProjectType || cluster == types.EmptyUUID || id == types.EmptyUUID {
		return nil, fmt.Errorf("instance type, cluster and uuid cannot be empty, get (%v, %v, %v)", typ, cluster, id)
	}

	key := etcd.GetInstancePath(r.env, typ, cluster, id)

	store, err := getStore()
	if err != nil {
		return nil, err
	}

	ret := &types.Instance{}
	err = store.Get(context.TODO(), key, ret, false)
	return ret, err
}

// ListInstancesOfTypeOnHost  list instances of type typ and host hostID
func (r *Registry) ListInstancesOfTypeOnHost(typ types.ProjectType, hostID types.UUID) (map[types.UUID]*types.Instance, error) {
	if typ == types.EmptyProjectType || hostID == types.EmptyUUID {
		return nil, fmt.Errorf("instance type and hostID cannot be empty, get (%v, %v)", typ, hostID)
	}

	key := etcd.GetInstancePathPrefixOfType(r.env, typ)
	store, err := getStore()
	if err != nil {
		return nil, err
	}

	pred := generic.SelectionPredicate{
		Label: labels.Everything(),
		Field: fields.ParseSelectorOrDie("hostID==" + string(hostID)),
		GetAttrs: func(obj interface{}) (labels.Set, fields.Set, error) {
			ins := obj.(**types.Instance)
			return nil, fields.Set{"hostID": (string)((**ins).HostID)}, nil
		},
	}

	out := []*types.Instance{}
	err = store.List(context.TODO(), key, pred, &out)

	ret := map[types.UUID]*types.Instance{}
	for _, ins := range out {
		ret[ins.UUID] = ins
	}

	return ret, err
}

// SaveInstance saves an instance
func (r *Registry) SaveInstance(in *types.Instance) error {
	if in.ProjecType == types.EmptyProjectType || in.UUID == types.EmptyUUID || in.ClusterName == types.EmptyUUID {
		return fmt.Errorf("instance type, cluster  and  uuid cannot be empty, get (%v, %v,  %v)", in.ProjecType, in.ClusterName, in.UUID)
	}

	key := etcd.GetInstancePath(r.env, in.ProjecType, in.ClusterName, in.UUID)

	store, err := getStore()
	if err != nil {
		return err
	}

	ret := &types.Instance{}
	err = store.Update(context.TODO(), key, in, ret, 0)
	glog.V(10).Infof("update instances from :%v, to %v", ret, in)
	return err
}
