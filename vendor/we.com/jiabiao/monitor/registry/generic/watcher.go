package generic

import (
	"context"
	"reflect"
	"strings"
	"sync"

	"github.com/coreos/etcd/clientv3"
	etcdrpc "github.com/coreos/etcd/etcdserver/api/v3rpc/rpctypes"

	log "github.com/golang/glog"
	"we.com/jiabiao/monitor/registry/watch"
)

var (
	// We have set a buffer in order to reduce times of context switches.
	incomingBufSize = 100
	outgoingBufSize = 100
)

type watcher struct {
	client *clientv3.Client
	typ    reflect.Type
}

// watchChan implements watch.Interface.
type watchChan struct {
	watcher           *watcher
	key               string
	initialRev        int64
	recursive         bool
	internalFilter    FilterFunc
	ctx               context.Context
	cancel            context.CancelFunc
	incomingEventChan chan *event
	resultChan        chan watch.Event
	errChan           chan error
}

func newWatcher(client *clientv3.Client, typ reflect.Type) *watcher {
	return &watcher{
		client: client,
		typ:    typ,
	}
}

// Watch watches on a key and returns a watch.Interface that transfers relevant notifications.
// If rev is zero, it will return the existing object(s) and then start watching from
// the maximum revision+1 from returned objects.
// If rev is non-zero, it will watch events happened after given revision.
// If recursive is false, it watches on given key.
// If recursive is true, it watches any children and directories under the key, excluding the root key itself.
// pred must be non-nil. Only if pred matches the change, it will be returned.
func (w *watcher) Watch(ctx context.Context, key string, rev int64, recursive bool, pred SelectionPredicate) (watch.Interface, error) {
	if recursive && !strings.HasSuffix(key, "/") {
		key += "/"
	}
	wc := w.createWatchChan(ctx, key, rev, recursive, pred)
	go wc.run()
	return wc, nil
}

func (w *watcher) createWatchChan(ctx context.Context, key string, rev int64, recursive bool, pred SelectionPredicate) *watchChan {
	wc := &watchChan{
		watcher:           w,
		key:               key,
		initialRev:        rev,
		recursive:         recursive,
		internalFilter:    SimpleFilter(pred),
		incomingEventChan: make(chan *event, incomingBufSize),
		resultChan:        make(chan watch.Event, outgoingBufSize),
		errChan:           make(chan error, 1),
	}
	if pred.Label.Empty() && pred.Field.Empty() {
		// The filter doesn't filter out any object.
		wc.internalFilter = nil
	}
	wc.ctx, wc.cancel = context.WithCancel(ctx)
	return wc
}

func (wc *watchChan) run() {
	watchClosedCh := make(chan struct{})
	go wc.startWatching(watchClosedCh)

	var resultChanWG sync.WaitGroup
	resultChanWG.Add(1)
	go wc.processEvent(&resultChanWG)

	select {
	case err := <-wc.errChan:
		if err == context.Canceled {
			break
		}
		errResult := parseError(err)
		if errResult != nil {
			// error result is guaranteed to be received by user before closing ResultChan.
			select {
			case wc.resultChan <- *errResult:
			case <-wc.ctx.Done(): // user has given up all results
			}
		}
	case <-watchClosedCh:
	case <-wc.ctx.Done(): // user cancel
	}

	// We use wc.ctx to reap all goroutines. Under whatever condition, we should stop them all.
	// It's fine to double cancel.
	wc.cancel()

	// we need to wait until resultChan wouldn't be used anymore
	resultChanWG.Wait()
	close(wc.resultChan)
}

func (wc *watchChan) Stop() {
	wc.cancel()
}

func (wc *watchChan) ResultChan() <-chan watch.Event {
	return wc.resultChan
}

// sync tries to retrieve existing data and send them to process.
// The revision to watch will be set to the revision in response.
func (wc *watchChan) sync() error {
	opts := []clientv3.OpOption{}
	if wc.recursive {
		opts = append(opts, clientv3.WithPrefix())
	}
	getResp, err := wc.watcher.client.Get(wc.ctx, wc.key, opts...)
	if err != nil {
		return err
	}
	wc.initialRev = getResp.Header.Revision

	for _, kv := range getResp.Kvs {
		prevResp, err := wc.watcher.client.Get(wc.ctx, string(kv.Key), clientv3.WithRev(wc.initialRev), clientv3.WithSerializable())
		if err != nil {
			return err
		}
		var prevVal []byte
		if len(prevResp.Kvs) > 0 {
			prevVal = prevResp.Kvs[0].Value
		}

		e := parseKV(kv, prevVal)
		e.key = strings.TrimPrefix(e.key, wc.key)
		wc.sendEvent(e)
	}
	return nil
}

// startWatching does:
// - get current objects if initialRev=0; set initialRev to current rev
// - watch on given key and send events to process.
func (wc *watchChan) startWatching(watchClosedCh chan struct{}) {
	if wc.initialRev == 0 {
		if err := wc.sync(); err != nil {
			log.Errorf("failed to sync with latest state: %v", err)
			wc.sendError(err)
			return
		}
	}
	opts := []clientv3.OpOption{clientv3.WithRev(wc.initialRev + 1), clientv3.WithPrevKV()}
	if wc.recursive {
		opts = append(opts, clientv3.WithPrefix())
	}
	wch := wc.watcher.client.Watch(wc.ctx, wc.key, opts...)
	for wres := range wch {
		if wres.Err() != nil {
			err := wres.Err()
			// If there is an error on server (e.g. compaction), the channel will return it before closed.
			log.Errorf("watch chan error: %v", err)
			wc.sendError(err)
			return
		}
		for _, e := range wres.Events {
			wc.sendEvent(parseEvent(e))
		}
	}
	// When we come to this point, it's only possible that client side ends the watch.
	// e.g. cancel the context, close the client.
	// If this watch chan is broken and context isn't cancelled, other goroutines will still hang.
	// We should notify the main thread that this goroutine has exited.
	close(watchClosedCh)
}

// processEvent processes events from etcd watcher and sends results to resultChan.
func (wc *watchChan) processEvent(wg *sync.WaitGroup) {
	defer wg.Done()

	for {
		select {
		case e := <-wc.incomingEventChan:
			res := wc.transform(e)
			if res == nil {
				continue
			}
			if len(wc.resultChan) == outgoingBufSize {
				log.Warningf("Fast watcher, slow processing. Number of buffered events: %d."+
					"Probably caused by slow dispatching events to watchers", outgoingBufSize)
			}
			// If user couldn't receive results fast enough, we also block incoming events from watcher.
			// Because storing events in local will cause more memory usage.
			// The worst case would be closing the fast watcher.
			select {
			case wc.resultChan <- *res:
			case <-wc.ctx.Done():
				return
			}
		case <-wc.ctx.Done():
			return
		}
	}
}

func (wc *watchChan) filter(obj interface{}) bool {
	if wc.internalFilter == nil {
		return true
	}
	return wc.internalFilter(obj)
}

func (wc *watchChan) acceptAll() bool {
	return wc.internalFilter == nil
}

// transform transforms an event into a result for user if not filtered.
func (wc *watchChan) transform(e *event) (res *watch.Event) {
	curObj, oldObj, err := wc.prepareObjs(e)
	if err != nil {
		log.Errorf("failed to prepare current and previous objects: %v", err)
		wc.sendError(err)
		return nil
	}

	switch {
	case e.isDeleted:
		if !wc.filter(oldObj) {
			return nil
		}
		res = &watch.Event{
			Type:   watch.Deleted,
			Key:    e.key,
			Object: oldObj,
		}
	case e.isCreated:
		if !wc.filter(curObj) {
			return nil
		}
		res = &watch.Event{
			Type:   watch.Added,
			Key:    e.key,
			Object: curObj,
		}
	default:
		if wc.acceptAll() {
			res = &watch.Event{
				Type:   watch.Modified,
				Key:    e.key,
				Object: curObj,
			}
			return res
		}
		curObjPasses := wc.filter(curObj)
		oldObjPasses := wc.filter(oldObj)
		switch {
		case curObjPasses && oldObjPasses:
			res = &watch.Event{
				Type:   watch.Modified,
				Key:    e.key,
				Object: curObj,
			}
		case curObjPasses && !oldObjPasses:
			res = &watch.Event{
				Type:   watch.Added,
				Key:    e.key,
				Object: curObj,
			}
		case !curObjPasses && oldObjPasses:
			res = &watch.Event{
				Type:   watch.Deleted,
				Key:    e.key,
				Object: oldObj,
			}
		}
	}
	return res
}

func parseError(err error) *watch.Event {
	var status *StorageError
	switch {
	case err == etcdrpc.ErrCompacted:
		status = &StorageError{
			Code:               ErrCodeUnreachable,
			AdditionalErrorMsg: err.Error(),
		}
	default:
		status = &StorageError{
			Code:               ErrCodeUnknown,
			AdditionalErrorMsg: err.Error(),
		}
	}

	return &watch.Event{
		Type:   watch.Error,
		Object: status,
	}
}

func (wc *watchChan) sendError(err error) {
	select {
	case wc.errChan <- err:
	case <-wc.ctx.Done():
	}
}

func (wc *watchChan) sendEvent(e *event) {
	if len(wc.incomingEventChan) == incomingBufSize {
		log.Warningf("Fast watcher, slow processing. Number of buffered events: %d."+
			"Probably caused by slow decoding, user not receiving fast, or other processing logic",
			incomingBufSize)
	}
	select {
	case wc.incomingEventChan <- e:
	case <-wc.ctx.Done():
	}
}

func (wc *watchChan) prepareObjs(e *event) (interface{}, interface{}, error) {
	var oldObj interface{}
	var curObj interface{}

	var err error
	if !e.isDeleted {
		curObj = reflect.New(wc.watcher.typ).Interface()
		err = decodeObj(e.value, curObj, e.rev)
		if err != nil {
			return nil, nil, err
		}
	}
	// We need to decode prevValue, only if this is deletion event or
	// the underlying filter doesn't accept all objects (otherwise we
	// know that the filter for previous object will return true and
	// we need the object only to compute whether it was filtered out
	// before).
	if len(e.prevValue) > 0 && (e.isDeleted || !wc.acceptAll()) {
		// Note that this sends the *old* object with the etcd revision for the time at
		// which it gets deleted.
		oldObj = reflect.New(wc.watcher.typ).Interface()
		err = decodeObj(e.prevValue, oldObj, e.rev)
		if err != nil {
			return nil, nil, err
		}
	}
	return curObj, oldObj, nil
}

func decodeObj(data []byte, into interface{}, rev int64) error {
	err := decode(data, into)
	return err
}
