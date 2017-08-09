package clusters

import (
	"fmt"

	log "github.com/golang/glog"
	"golang.org/x/net/context"
	"we.com/jiabiao/monitor/core/etcd"
	"we.com/jiabiao/monitor/core/types"
	"we.com/jiabiao/monitor/registry/generic"
)

// NewRegistry  return a new *Registry
func NewRegistry(env types.ENV) *Registry {
	return &Registry{
		env:   env,
		stopC: make(chan struct{}),
	}
}

// Registry  is  a simple client to  retrieve project info from store
type Registry struct {
	env   types.ENV
	stopC chan struct{}
}

// Stop  close chan  and stops watch  routines
func (r *Registry) Stop() {
	close(r.stopC)
}

func getStore() (generic.Interface, error) {
	return generic.GetStoreInstance(etcd.ProjectBasePrefix, false)
}

// GetProjectInfo reture internal project config, cluster uniquely indentifies which project
func (r *Registry) GetProjectInfo(typ types.ProjectType, cluster types.UUID) (*types.ClusterInfo, error) {
	store, err := getStore()
	if err != nil {
		log.Errorf("get project info error: %s", err)
		return nil, err
	}

	key := etcd.GetClusterInfoPath(r.env, typ, cluster)
	out := types.ClusterInfo{}

	if err = store.Get(context.TODO(), key, &out, false); err != nil {
		return nil, err
	}

	return &out, nil
}

// DelProjectInfo del project  config
func (r *Registry) DelProjectInfo(ptype types.ProjectType, cluster types.UUID) (*types.ClusterInfo, error) {
	if ptype == types.EmptyProjectType || cluster == types.EmptyUUID {
		return nil, fmt.Errorf("project and project type cannot be empty, get (%v, %v)", cluster, ptype)
	}

	key := etcd.GetClusterInfoPath(r.env, ptype, cluster)
	store, err := getStore()
	if err != nil {
		return nil, err
	}

	out := &types.ClusterInfo{}
	err = store.Delete(context.TODO(), key, out)
	if err != nil {
		return nil, err
	}

	return out, nil
}

// SaveProjectInfo  create or update an projectinfo
func (r *Registry) SaveProjectInfo(p *types.ClusterInfo) error {

	if p.Type == types.EmptyProjectType || p.ClusterName == types.EmptyUUID {
		return fmt.Errorf("project and project type cannot be empty, get (%v, %v)", p.ClusterName, p.Type)
	}

	key := etcd.GetClusterInfoPath(r.env, p.Type, p.ClusterName)

	// ensure config is valid
	if err := p.ValidateConfig(); err != nil {
		return err
	}

	store, err := getStore()
	if err != nil {
		return err
	}

	return store.Update(context.TODO(), key, p, nil, 0)
}

// ListProjectTypes list all known project types
func (r *Registry) ListProjectTypes() ([]types.ProjectType, error) {
	return types.GetKnownProjectTypes(), nil
}

// ListClusterNamesOfType returns cluster name of type ptype
func (r *Registry) ListClusterNamesOfType(ptype types.ProjectType) ([]types.UUID, error) {
	if ptype == "" {
		return nil, fmt.Errorf("project type cannot be empty, get (%v)", ptype)
	}

	key := etcd.GetClusterInfoTypePrefix(r.env, ptype)
	store, err := getStore()
	if err != nil {
		return nil, err
	}

	clusters, err := store.ListKeys(context.TODO(), key)
	if err != nil {
		return nil, err
	}
	ret := make([]types.UUID, len(clusters))
	for i, c := range clusters {
		ret[i] = types.UUID(c)
	}

	return ret, nil
}
