package generic

import (
	"fmt"
	"sync"

	"we.com/jiabiao/common/etcd"

	"github.com/coreos/etcd/clientv3"
	"github.com/golang/glog"
	"golang.org/x/net/context"
)

// DestroyFunc  close etcdClient  and related resources
type DestroyFunc func()

func newEtcdClient(cfg clientv3.Config) (*clientv3.Client, DestroyFunc, error) {
	client, err := clientv3.New(cfg)
	if err != nil {
		return nil, nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	StartCompactor(ctx, client)

	destroyFunc := func() {
		cancel()
		client.Close()
	}

	return client, destroyFunc, nil
}

var (
	clientConfig *clientv3.Config
	etcdClient   *clientv3.Client
	destroyFunc  DestroyFunc
	cliLock      sync.RWMutex
)

// IsInitialized  return true if this  package is intialized
func IsInitialized() bool {
	return clientConfig != nil
}

// SetEtcdConfig  set etcd config, this function can only called once at the server start time
func SetEtcdConfig(cfg etcd.Config) {
	if clientConfig != nil {
		glog.Fatalf("set etcd config, config is not empty")
	}
	cliLock.Lock()
	defer cliLock.Unlock()
	ncfg := clientv3.Config(cfg)
	clientConfig = &ncfg
}

// GetStoreInstance returns an Instance of store, or err if config is ni or error happens when create a new etcd client
func GetStoreInstance(prefix string, quorumRead bool) (Interface, error) {
	// quick path
	err := dialClient(etcdClient)
	if err == nil {
		if quorumRead {
			return New(etcdClient, prefix), nil
		}

		return NewWithNoQuorumRead(etcdClient, prefix), nil
	}

	cliLock.Lock()
	defer cliLock.Unlock()

	// here etcdClient may be just created,  so we need to check again
	if etcdClient != nil {
		if err = dialClient(etcdClient); err != nil {
			etcdClient.Close()
			destroyFunc()
		} else {
			if quorumRead {
				return New(etcdClient, prefix), nil
			}

			return NewWithNoQuorumRead(etcdClient, prefix), nil
		}
	}

	// create a new instance
	if clientConfig == nil {
		return nil, fmt.Errorf("etcd client config is nil")
	}

	cli, desfunc, err := newEtcdClient(*clientConfig)
	if err != nil {
		return nil, err
	}

	etcdClient = cli
	destroyFunc = desfunc

	if quorumRead {
		return New(etcdClient, prefix), nil
	}

	return NewWithNoQuorumRead(etcdClient, prefix), nil
}

func dialClient(cli *clientv3.Client) error {
	if cli == nil {
		return fmt.Errorf("client is nil")
	}

	resp, err := etcdClient.MemberList(context.Background())
	if err != nil {
		glog.Errorf("%v", err)
		return err
	}

	if len(resp.Members) > 0 {
		return nil
	}

	err = fmt.Errorf("dial etcd client, got unexpected value： 0")
	glog.Errorf("%s", err.Error())
	return err
}
