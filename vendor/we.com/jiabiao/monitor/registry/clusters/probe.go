package clusters

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/golang/glog"

	"we.com/jiabiao/monitor/core/etcd"
	"we.com/jiabiao/monitor/core/java"
	"we.com/jiabiao/monitor/core/types"
	"we.com/jiabiao/monitor/registry/generic"
	"we.com/jiabiao/monitor/registry/watch"
)

type JavaProbRegister struct{}

func (jpr *JavaProbRegister) getStore() (generic.Interface, error) {
	return generic.GetStoreInstance(filepath.Join(
		etcd.DeployBasePrefix, etcd.GetProbePrefix(java.Type)), false)
}

// Get get probconfig of project
func (jpr *JavaProbRegister) Get(project string) (*java.ProbeConfig, error) {
	store, err := jpr.getStore()
	if err != nil {
		return nil, err
	}

	ret := &java.ProbeConfig{}
	err = store.Get(context.Background(), project, ret, false)
	if err == nil {
		return ret, nil
	}
	if generic.IsNotFound(err) {
		return nil, nil
	}
	return nil, err
}

// Save insert or update  project config
func (jpr *JavaProbRegister) Save(pc *java.ProbeConfig) error {
	if pc == nil {
		return nil
	}

	key := pc.Project
	if key == "" {
		return errors.New("project cannot be empty")
	}

	store, err := jpr.getStore()
	if err != nil {
		return err
	}

	return store.Update(context.Background(), key, pc, nil, 0)
}

// Del  delete probeconfig
func (jpr *JavaProbRegister) Del(project string) error {
	store, err := jpr.getStore()
	if err != nil {
		return err
	}

	return store.Delete(context.Background(), project, nil)
}

func (jpr *JavaProbRegister) Watch(ctx context.Context, handler watch.EventHandler) error {
	store, err := jpr.getStore()
	if err != nil {
		glog.Errorf("getStoreInstance error: %s", err)
		return err
	}

	key := ""
	watcher, err := store.Watch(ctx, key, generic.Everything, true, reflect.TypeOf(types.HostReplicaSpec{}))
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
				event.Key = strings.TrimPrefix(event.Key, filepath.Join(
					etcd.DeployBasePrefix, etcd.GetProbePrefix(java.Type)))
				if strings.HasPrefix(event.Key, "/") {
					event.Key = strings.TrimPrefix(event.Key, "/")
				}
				if err := handler(event); err != nil {
					glog.Fatalf("handle host replica spec event: %v", err)
				}
			}
		case <-ctx.Done():
			watcher.Stop()
			return nil
		}
	}
}
