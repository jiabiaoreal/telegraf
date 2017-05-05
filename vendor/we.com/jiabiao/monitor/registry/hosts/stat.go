package hosts

import (
	"context"

	"github.com/golang/glog"

	"we.com/jiabiao/monitor/core/etcd"
	"we.com/jiabiao/monitor/core/types"
)

// NewRegistry  returns a registry
func NewRegistry(env types.ENV) *Registry {
	return &Registry{
		env:   env,
		stopC: make(chan struct{}),
	}
}

// Registry client
type Registry struct {
	env   types.ENV
	stopC chan struct{}
}

// Stop stops the register, and watch if has any
func (r *Registry) Stop() {
	close(r.stopC)
}

// GetResource return resource usage of a host
func (r *Registry) GetResource(uuid types.UUID) (*types.HostStatus, error) {
	key := etcd.GetHostStatPath(r.env, uuid)

	store, err := getStore()
	if err != nil {
		return nil, err
	}

	out := &types.HostStatus{}
	err = store.Get(context.TODO(), key, out, false)

	return out, err
}

// UpdateResource return resource info of a host
func (r *Registry) UpdateResource(hr *types.HostStatus) (*types.HostStatus, error) {
	key := etcd.GetHostStatPath(r.env, hr.HostID)

	store, err := getStore()
	if err != nil {
		return nil, err
	}

	// since  update is more frequent than create, so first try to update
	// if Node not exist, then create
	// hostResource  have a ttl of 2 mins
	err = store.Update(context.TODO(), key, hr, nil, 2*60)

	return hr, err
}

// ListHostInfoUUIDs returns  hostid
func (r *Registry) ListHostInfoUUIDs() ([]types.UUID, error) {
	ids := []types.UUID{}

	key := etcd.GetHostStatPath(r.env, "")

	store, err := getStore()
	if err != nil {
		return nil, err
	}

	ks, err := store.ListKeys(context.TODO(), key)
	if err != nil {
		return nil, err
	}

	for _, k := range ks {
		ids = append(ids, types.UUID(k))
	}

	glog.V(10).Infof("listhost uuids: %+v", ids)

	return ids, nil
}

// GetHostInfoOfHostID return hostinfo of hostID,  or error
func (r *Registry) GetHostInfoOfHostID(hostID types.UUID) (*types.HostInfo, error) {
	store, err := getStore()
	if err != nil {
		return nil, err
	}

	key := etcd.GetHostInfoPath(r.env, hostID)

	ret := types.HostInfo{}

	if err = store.Get(context.TODO(), key, &ret, false); err != nil {
		return nil, err
	}

	return &ret, nil
}

// SaveHostInfoOf  save or update   hostinfo of hostInfo.HostID, or err
func (r *Registry) SaveHostInfoOf(hi *types.HostInfo) error {
	store, err := getStore()
	if err != nil {
		return err
	}

	key := etcd.GetHostInfoPath(r.env, hi.HostID)

	return store.Update(context.TODO(), key, hi, nil, 0)
}

// DelHostInfo delete hostInfo from store
func (r *Registry) DelHostInfo(hostID types.UUID) error {
	store, err := getStore()
	if err != nil {
		return err
	}

	key := etcd.GetHostInfoPath(r.env, hostID)
	return store.Delete(context.TODO(), key, nil)
}
