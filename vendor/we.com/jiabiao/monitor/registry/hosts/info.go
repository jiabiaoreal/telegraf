package hosts

import (
	"context"
	"fmt"
	"reflect"

	"github.com/golang/glog"
	sync "github.com/golang/sync/syncmap"
	multierror "github.com/hashicorp/go-multierror"

	"we.com/jiabiao/monitor/core/etcd"
	"we.com/jiabiao/monitor/core/types"
	"we.com/jiabiao/monitor/registry/generic"
	"we.com/jiabiao/monitor/registry/watch"
)

func getStore() (generic.Interface, error) {
	return generic.GetStoreInstance(etcd.HostBasePrefix, false)
}

// NewCachedRegistery returns a cachedRegistry
func NewCachedRegistery(env types.ENV) *CachedRegistry {
	ret := &CachedRegistry{
		env:         env,
		hostInfoMap: sync.Map{},
		stopC:       make(chan struct{}),
	}

	ctx := context.Background()
	go ret.watchHostInfo(ctx)

	return ret
}

// CachedRegistry only caches  hostinfo
type CachedRegistry struct {
	env types.ENV
	// since the num of host is very limited, so we can cache all hosts info here
	// we load all host info from config,  and watch store from changes
	// keys is HostId, value is  *types.HostInfo
	hostInfoMap sync.Map

	stopC chan struct{}
}

func (r *CachedRegistry) watchHostInfo(ctx context.Context) error {
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

	for {
		select {
		case event := <-watcher.ResultChan():
			if err := r.handelEvent(event); err != nil {
				glog.Fatalf("handle hostinfo event: %v", err)
				return err
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

func (r *CachedRegistry) handelEvent(event watch.Event) error {
	if event.Type == watch.Error {
		err, ok := event.Object.(error)
		if !ok {
			glog.Warningf("event type if error, but event.Object is not an error")
			err = fmt.Errorf("watch got error :%v", event.Object)
		}
		glog.Warningf("watch err: %v", err)

		return nil
	}

	dat, ok := event.Object.(*types.HostInfo)
	if !ok {
		glog.Fatalf("event object must be an instance of types.HostIno, got %v", event.Object)
	}
	hostid := dat.HostID
	if len(hostid) == 0 {
		glog.Fatal("hostid cannot be empty")
	}

	switch event.Type {
	case watch.Deleted:
		glog.V(10).Infof("hostinfo is deleted: %v", dat)
		r.hostInfoMap.Delete(hostid)

		return nil

	case watch.Added, watch.Modified:
		old, _ := r.hostInfoMap.Load(hostid)
		glog.V(10).Infof("hostinfo update: from %v to %v", old, dat)
		r.hostInfoMap.Store(hostid, dat)
		return nil
	default:
		glog.Errorf("unkonw eventy type: %v", event.Type)
		return nil
	}
}

// GetHostInfo  get host info from cache
func (r *CachedRegistry) GetHostInfo(uuid types.UUID) (*types.HostInfo, error) {
	if v, ok := r.hostInfoMap.Load(uuid); ok {
		return v.(*types.HostInfo), nil
	}
	return nil, nil
}

// ListHostInfo list host info stored in cache
func (r *CachedRegistry) ListHostInfo(filter generic.SelectionPredicate) ([]types.HostInfo, error) {
	hi := []types.HostInfo{}

	var merr *multierror.Error
	r.hostInfoMap.Range(func(key, value interface{}) bool {
		ok, err := filter.Matches(value)
		if err != nil {
			merr = multierror.Append(merr, err)
			return true
		}
		if ok {
			hiv := value.(*types.HostInfo)
			hi = append(hi, *hiv)
		}

		return true
	})

	return hi, merr.ErrorOrNil()
}

// SaveHostInfo save host info to store and update local cache
func (r *CachedRegistry) SaveHostInfo(hi *types.HostInfo) (*types.HostInfo, error) {
	if string(hi.HostID) == "" {
		return nil, fmt.Errorf("hostID is empyt")
	}

	tmp, ok := r.hostInfoMap.Load(hi.HostID)
	if !ok {
		if hi.ENV == types.ENV("") {
			return nil, fmt.Errorf("env of hi shouldnot empty: %v", hi)
		}

		tmp = &types.HostInfo{
			ENV: hi.ENV,
		}
	}

	ret := tmp.(*types.HostInfo)
	if hi.ENV == "" {
		hi.ENV = ret.ENV
	} else {
		if hi.ENV != ret.ENV {
			err := fmt.Errorf("host (%s:%s) env cannot change: %v -> %v", ret.HostID, ret.HostName, ret.ENV, hi.ENV)
			glog.Errorf("%v", err)
			return nil, err
		}
	}

	store, err := getStore()
	if err != nil {
		return nil, err
	}

	key := etcd.GetHostInfoPath(r.env, hi.HostID)

	if err := store.Update(context.TODO(), key, hi, nil, 0); err != nil {
		return nil, err
	}

	// uddate cache
	r.hostInfoMap.Store(hi.HostID, hi)

	// shadow copy a new instance
	return &(*ret), nil
}

// DelHostInfoByUUID  delete hostinfo
func (r *CachedRegistry) DelHostInfoByUUID(uuid types.UUID) (*types.HostInfo, error) {
	v, ok := r.hostInfoMap.Load(uuid)
	// if uuid not exists
	if !ok {
		return nil, nil
	}

	ret := v.(*types.HostInfo)

	key := etcd.GetHostInfoPath(r.env, uuid)

	store, err := getStore()
	if err != nil {
		return nil, err
	}

	out := &types.HostInfo{}
	err = store.Delete(context.TODO(), key, out)
	if err != nil {
		return nil, err
	}

	r.hostInfoMap.Delete(uuid)

	return ret, err
}
