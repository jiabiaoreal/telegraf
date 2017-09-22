package replicas

import (
	"context"
	"fmt"
	"log"
	"reflect"
	"sync"
	"time"

	"we.com/jiabiao/common/alert"

	etcddir "we.com/jiabiao/monitor/core/etcd"
	core "we.com/jiabiao/monitor/core/types"
	"we.com/jiabiao/monitor/registry/generic"
	"we.com/jiabiao/monitor/registry/watch"

	"github.com/coreos/etcd/clientv3"
	"github.com/coreos/pkg/capnslog"
	"github.com/golang/glog"

	"github.com/influxdata/telegraf/internal/hostinfo"
	"github.com/influxdata/telegraf/plugins/inputs/monitor/java"
	psm "github.com/influxdata/telegraf/plugins/inputs/monitor/process"
	"github.com/influxdata/telegraf/plugins/inputs/monitor/types"
)

func getWatchPath() string {
	env := core.ENV(hostinfo.GetEnv())
	return etcddir.GetHostReplicaSpecPath(env, hostinfo.GetHostID())
}

type reportOpts struct {
	EtcdKeepalivedInterval time.Duration
	EtcdKeepFlushInterval  time.Duration
}

// HostReplicaSpecManager manager ExpectedReplicaSpec and  ActualReplicaSpec, and make sure the fit each other
// it also watches etcd, for  expected config update
type HostReplicaSpecManager struct {
	ExpectedReplicaSpec *core.HostReplicaSpec
	UpdateTime          time.Time
	reportOpts          reportOpts
}

// NewHostReplicaSpecManager  return a new *HostReplicaSpecManager  or error
func NewHostReplicaSpecManager() (*HostReplicaSpecManager, error) {
	var plog = capnslog.NewPackageLogger("github.com/coreos/etcd", "clientv3")
	clientv3.SetLogger(plog)

	// register monitors
	javaMonitor := java.NewStateMonitor()

	ret := &HostReplicaSpecManager{
		reportOpts: reportOpts{
			EtcdKeepalivedInterval: 2 * time.Second,
			EtcdKeepFlushInterval:  20 * time.Second,
		},
	}
	return ret, nil
}

// ReLoadExpectedSpec load current HostReplicaSpec from  ectd
func (hrsm *HostReplicaSpecManager) ReLoadExpectedSpec() error {
	hrs, err := hrsm.hostClient.GetReplicaSpec(hostinfo.GetHostID())
	if err != nil {
		se, ok := err.(*generic.StorageError)
		if ok && se.Code == generic.ErrCodeKeyNotFound {
			glog.Info("replica spec not exist, set replica spec to current running instances")
			if err := hrsm.getActualReplicaSpec(); err != nil {
				glog.Errorf("update actual host replica spec err: %v", err)
				return err
			}

			hrs = &core.HostReplicaSpec{}
			hrsm.lock.Lock()
			hrsm.ActualReplicaSpec.Range(func(pt core.ProjectType, tv *core.TypeReplicaSpec) bool {
				tv.Range(func(clustername core.UUID, cv *core.ClusterReplicaSpec) bool {

					hrs.AddCluserSpec(*cv)
					return true
				})
				return true
			})
			hrsm.lock.Unlock()

			// update etcd config
			hrsm.hostClient.SetReplicaSpec(hostinfo.GetHostID(), hrs)
		} else {
			return err
		}
	}

	hrsm.lock.Lock()
	defer hrsm.lock.Unlock()
	hrsm.ExpectedReplicaSpec = hrs
	hrsm.ExpectedUpdateTime = time.Now()
	return nil
}

func (hrsm *HostReplicaSpecManager) handleHostSpecEvent(event watch.Event) error {
	glog.Infof("receve event: %v", event)
	defer func() {
		tags := psm.GetCommonReportTags()
		metric := map[string]interface{}{
			"type": event.Type,
		}

		hrsm.acc.AddFields("replicaWatch", metric, tags)
	}()

	dat, ok := event.Object.(*core.HostReplicaSpec)
	glog.V(10).Infof("typeof event data: %v", reflect.TypeOf(event.Object))
	if !ok {
		alert.SendMsg(fmt.Sprintf("host replica spec watch: unexpected object type: %v ", event))
		glog.Fatalf("event object must be an instance of types.HostIno, got %v", event.Object)
	}

	hostid := hostinfo.GetHostID()
	switch event.Type {
	case watch.Deleted:
		alert.SendMsg(fmt.Sprintf("host replica spec deleted %v, event: %v", hostid, dat))
		glog.Errorf("host replica spec is deleted: %v", dat)

	case watch.Added, watch.Modified:
		old := hrsm.ExpectedReplicaSpec
		new := dat
		glog.Infof("host replica spec changed from %v to %v", old, new)
		add, remove := core.DiffHostReplicaSpec(old, new)
		glog.Infof("changes: add: %v, remove: %v", add, remove)

		hrsm.ExpectedReplicaSpec = new
		return nil

	case watch.Error:
		err, ok := event.Object.(error)
		if !ok {
			glog.Warningf("event type if error, but event.Object is not an error")
			err = fmt.Errorf("watch got error :%v", event.Object)
		}
		glog.Warningf("watch err: %v", err)

		return nil
	}
	return nil
}

// WatchExpectedSpecUpdate watchs etcd for expected spec update
func (hrsm *HostReplicaSpecManager) WatchExpectedSpecUpdate() error {
	if hrsm == nil || hrsm.hostClient == nil {
		return fmt.Errorf("expectation watch not initialized")
	}

	if hrsm.hostClient == nil {
		log.Fatal("watch host relica spec: etcd client cannot be nil")
	}

	go hrsm.hostClient.WatchHostReplicaSpec(context.TODO(), hostinfo.GetHostID(), hrsm.handleHostSpecEvent)

	go func() {
		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := hrsm.ReLoadExpectedSpec(); err != nil {
					glog.Errorf("monitor: reload host replica err: %v", err)
				}
			case <-hrsm.stopC:
				return
			}
		}
	}()

	return nil
}

func (hrsm *HostReplicaSpecManager) getActualReplicaSpec() error {
	actualSpec := &core.HostReplicaSpec{}
	for typ, monitor := range hrsm.monitorItems {
		ps, err := monitor.GetProcessPidList()
		if err != nil {
			glog.Errorf("error get process of type %v, %v", typ, err)
		}
		for _, p := range ps {
			clustername, version := p.GetClusterNameAndVersion()
			actualSpec.AddCluserSpec(core.ClusterReplicaSpec{
				Type:         core.ProjectType(typ),
				ClusterName:  clustername,
				Version:      version,
				InstancesNum: 1,
			})
		}
	}
	hrsm.lock.Lock()
	hrsm.ActualReplicaSpec = actualSpec
	hrsm.ActualUpdateTime = time.Now()
	hrsm.lock.Unlock()

	return nil
}

// Probe  probes processinfo status
func (hrsm *HostReplicaSpecManager) Probe() {
	psm.Start()
	timer := time.NewTimer(0)
	for {
		select {
		case <-timer.C:
			glog.V(15).Info("monitor: start to probe")

			wg := sync.WaitGroup{}
			for typ, monitor := range hrsm.monitorItems {
				ps, err := monitor.GetProcessPidList()
				glog.V(10).Infof("end get process list of %v", typ)
				if err != nil {
					glog.Errorf("error get process of type %v, %v", typ, err)
				}

				wg.Add(len(ps))
				for _, p := range ps {
					time.Sleep(20 * time.Millisecond)
					tags := psm.GetCommonReportTags()
					c, v := p.GetClusterNameAndVersion()
					tags["cluster"] = string(c)
					tags["version"] = string(v)
					tags["project_type"] = string(typ)
					go func(ps types.ProcessInfor) {
						defer wg.Done()
						res, msg := ps.Probe()
						hrsm.acc.AddFields("probe", map[string]interface{}{"result": res, "node": ps.GetNodeID(), "message": msg}, tags)
					}(p)
				}
			}
			wg.Wait()
			glog.V(15).Info("monitor: end probe")
			timer.Reset(10 * time.Second)
		case <-hrsm.stopC:
			return
		}
	}
}
