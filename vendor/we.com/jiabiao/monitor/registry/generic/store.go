package generic

import (
	"encoding/json"
	"fmt"
	"path"
	"reflect"
	"strings"

	"github.com/coreos/etcd/clientv3"
	"golang.org/x/net/context"
	"we.com/jiabiao/monitor/registry/watch"
)

// EnforcePtr ensures that obj is a pointer of some sort. Returns a reflect.Value
// of the dereferenced pointer, ensuring that it is settable/addressable.
// Returns an error if this is not possible.
func EnforcePtr(obj interface{}) (reflect.Value, error) {
	v := reflect.ValueOf(obj)
	if v.Kind() != reflect.Ptr {
		if v.Kind() == reflect.Invalid {
			return reflect.Value{}, fmt.Errorf("expected pointer, but got invalid kind")
		}
		return reflect.Value{}, fmt.Errorf("expected pointer, but got %v type", v.Type())
	}
	if v.IsNil() {
		return reflect.Value{}, fmt.Errorf("expected pointer, but got nil")
	}
	return v.Elem(), nil
}

type store struct {
	client *clientv3.Client
	// getOpts contains additional options that should be passed
	// to all Get() calls.
	getOps     []clientv3.OpOption
	pathPrefix string
	watcher    *watcher
}

type elemForDecode struct {
	data []byte
	rev  uint64
}

// ResponseMeta contains information about the database metadata that is associated with
// an object. It abstracts the actual underlying objects to prevent coupling with concrete
// database and to improve testability.
type ResponseMeta struct {
	// TTL is the time to live of the node that contained the returned object. It may be
	// zero or negative in some cases (objects may be expired after the requested
	// expiration time due to server lag).
	TTL int64
	// The resource version of the node that contained the returned object.
	ResourceVersion uint64
}

type objState struct {
	obj  interface{}
	meta *ResponseMeta
	rev  int64
	data []byte
}

// Interface offers a common interface for object marshaling/unmarshaling operations and
// hides all the storage-related operations behind it.
type Interface interface {
	// Create adds a new object at a key unless it already exists. 'ttl' is time-to-live
	// in seconds (0 means forever). If no error is returned and out is not nil, out will be
	// set to the read value from database.
	Create(ctx context.Context, key string, obj, out interface{}, ttl uint64) error

	// Delete removes the specified key and returns the value that existed at that spot.
	// If key didn't exist, it will return NotFound storage error.
	Delete(ctx context.Context, key string, out interface{}) error

	// Watch begins watching the specified key. Events are decoded into API objects,
	// and any items selected by 'p' are sent down to returned watch.Interface.
	// resourceVersion may be used to specify what version to begin watching,
	// which should be the current resourceVersion, and no longer rv+1
	// (e.g. reconnecting without missing any updates).
	Watch(ctx context.Context, key string, p SelectionPredicate, recursive bool, expectTyp reflect.Type) (watch.Interface, error)

	// Get unmarshals json found at key into objPtr. On a not found error, will either
	// return a zero object of the requested type, or an error, depending on ignoreNotFound.
	// Treats empty responses and nil response nodes exactly like a not found error.
	Get(ctx context.Context, key string, objPtr interface{}, ignoreNotFound bool) error

	// List unmarshalls jsons found at directory defined by key and opaque them
	// into *List api object (an object that satisfies runtime.IsList definition).
	// The returned contents may be delayed, but it is guaranteed that they will
	// be have at least 'resourceVersion'.
	List(ctx context.Context, key string, p SelectionPredicate, listObj interface{}) error

	// ListKeys  ls keys under a  prefix
	ListKeys(ctx context.Context, key string) ([]string, error)
	Update(ctx context.Context, key string, in, out interface{}, ttl int64) error
}

// New returns an etcd3 implementation of storage.Interface.
func New(c *clientv3.Client, prefix string) Interface {
	return newStore(c, true, prefix, nil)
}

// NewWithNoQuorumRead returns etcd3 implementation of storage.Interface
// where Get operations don't require quorum read.
func NewWithNoQuorumRead(c *clientv3.Client, prefix string) Interface {
	return newStore(c, false, prefix, nil)
}

func newStore(c *clientv3.Client, quorumRead bool, prefix string, defaultTyp reflect.Type) *store {
	result := &store{
		client:     c,
		pathPrefix: prefix,
		watcher: &watcher{
			client: c,
			typ:    defaultTyp,
		},
	}
	if !quorumRead {
		// In case of non-quorum reads, we can set WithSerializable()
		// options for all Get operations.
		result.getOps = append(result.getOps, clientv3.WithSerializable())
	}
	return result
}

// Get implements storage.Interface.Get.
func (s *store) Get(ctx context.Context, key string, out interface{}, ignoreNotFound bool) error {
	key = keyWithPrefix(s.pathPrefix, key)
	getResp, err := s.client.KV.Get(ctx, key, s.getOps...)
	if err != nil {
		return err
	}

	if len(getResp.Kvs) == 0 {
		if ignoreNotFound {
			return SetZeroValue(out)
		}
		return NewKeyNotFoundError(key, 0)
	}
	kv := getResp.Kvs[0]
	return decode(kv.Value, out)
}

// Create implements storage.Interface.Create.
func (s *store) Create(ctx context.Context, key string, obj, out interface{}, ttl uint64) error {
	data, err := encode(obj)
	if err != nil {
		return err
	}
	key = keyWithPrefix(s.pathPrefix, key)

	opts, err := s.ttlOpts(ctx, int64(ttl))
	if err != nil {
		return err
	}

	txnResp, err := s.client.KV.Txn(ctx).If(
		notFound(key),
	).Then(
		clientv3.OpPut(key, string(data), opts...),
	).Commit()
	if err != nil {
		return err
	}
	if !txnResp.Succeeded {
		return NewKeyExistsError(key, 0)
	}

	if out != nil {
		return decode(data, out)
	}
	return nil
}

// Delete implements storage.Interface.Delete.
func (s *store) Delete(ctx context.Context, key string, out interface{}) error {
	_, err := EnforcePtr(out)
	if err != nil {
		panic("unable to convert output object to pointer")
	}
	key = keyWithPrefix(s.pathPrefix, key)
	return s.unconditionalDelete(ctx, key, out)
}

func (s *store) unconditionalDelete(ctx context.Context, key string, out interface{}) error {
	// We need to do get and delete in single transaction in order to
	// know the value and revision before deleting it.
	txnResp, err := s.client.KV.Txn(ctx).If().Then(
		clientv3.OpGet(key),
		clientv3.OpDelete(key),
	).Commit()
	if err != nil {
		return err
	}
	getResp := txnResp.Responses[0].GetResponseRange()
	if len(getResp.Kvs) == 0 {
		return NewKeyNotFoundError(key, 0)
	}

	kv := getResp.Kvs[0]
	return decode(kv.Value, out)
}

// List implements storage.Interface.List.
func (s *store) List(ctx context.Context, key string, pred SelectionPredicate, listObj interface{}) error {
	_, err := EnforcePtr(listObj)
	if err != nil {
		return err
	}
	key = keyWithPrefix(s.pathPrefix, key)
	// We need to make sure the key ended with "/" so that we only get children "directories".
	// e.g. if we have key "/a", "/a/b", "/ab", getting keys with prefix "/a" will return all three,
	// while with prefix "/a/" will return only "/a/b" which is the correct answer.
	if !strings.HasSuffix(key, "/") {
		key += "/"
	}
	getResp, err := s.client.KV.Get(ctx, key, clientv3.WithPrefix())
	if err != nil {
		return err
	}

	elems := make([]*elemForDecode, len(getResp.Kvs))
	for i, kv := range getResp.Kvs {
		elems[i] = &elemForDecode{
			data: kv.Value,
			rev:  uint64(kv.ModRevision),
		}
	}
	return decodeList(elems, SimpleFilter(pred), listObj)
}

// List implements storage.Interface.List.
func (s *store) ListKeys(ctx context.Context, key string) ([]string, error) {
	key = keyWithPrefix(s.pathPrefix, key)
	// We need to make sure the key ended with "/" so that we only get children "directories".
	// e.g. if we have key "/a", "/a/b", "/ab", getting keys with prefix "/a" will return all three,
	// while with prefix "/a/" will return only "/a/b" which is the correct answer.
	if !strings.HasSuffix(key, "/") {
		key += "/"
	}
	getResp, err := s.client.KV.Get(ctx, key, clientv3.WithPrefix(), clientv3.WithKeysOnly())
	if err != nil {
		return nil, err
	}

	precount := len(strings.Split(key, "/")) - 1
	retMap := map[string]bool{}

	for _, kv := range getResp.Kvs {
		ps := strings.Split(string(kv.Key), "/")
		retMap[ps[precount]] = true
	}

	elems := make([]string, len(retMap))
	i := 0
	for v := range retMap {
		elems[i] = v
		i++
	}
	return elems, nil
}

func (s *store) Update(ctx context.Context, key string, in, out interface{}, ttl int64) error {
	key = keyWithPrefix(s.pathPrefix, key)

	data, err := encode(in)
	if err != nil {
		return err
	}
	opts, err := s.ttlOpts(ctx, ttl)
	if err != nil {
		return err
	}

	if out != nil {
		opts = append(opts, clientv3.WithPrevKV())
	}

	putResp, err := s.client.KV.Put(ctx, key, string(data), opts...)
	if err != nil {
		return err
	}

	kv := putResp.PrevKv

	if out != nil && kv != nil {
		return decode(kv.Value, out)
	}
	return nil
}

// ttlOpts returns client options based on given ttl.
// ttl: if ttl is non-zero, it will attach the key to a lease with ttl of roughly the same length
func (s *store) ttlOpts(ctx context.Context, ttl int64) ([]clientv3.OpOption, error) {
	if ttl == 0 {
		return nil, nil
	}
	// TODO: one lease per ttl key is expensive. Based on current use case, we can have a long window to
	// put keys within into same lease. We shall benchmark this and optimize the performance.
	lcr, err := s.client.Lease.Grant(ctx, ttl)
	if err != nil {
		return nil, err
	}
	return []clientv3.OpOption{clientv3.WithLease(clientv3.LeaseID(lcr.ID))}, nil
}

func (s *store) Watch(ctx context.Context, key string, p SelectionPredicate, recursive bool, expectTyp reflect.Type) (watch.Interface, error) {
	if expectTyp != nil {
		s.watcher.typ = expectTyp
	}

	if s.watcher.typ == nil {
		return nil, fmt.Errorf("expectedType cannot be nil")
	}

	key = keyWithPrefix(s.pathPrefix, key)

	return s.watcher.Watch(ctx, key, 0, recursive, p)
}

func keyWithPrefix(prefix, key string) string {
	if strings.HasPrefix(key, prefix) {
		return key
	}
	return path.Join(prefix, key)
}

func encode(object interface{}) ([]byte, error) {
	return json.Marshal(object)
}

// decode decodes value of bytes into object. It will also set the object resource version to rev.
// On success, objPtr would be set to the object.
func decode(value []byte, objPtr interface{}) error {
	if _, err := EnforcePtr(objPtr); err != nil {
		panic("unable to convert output object to pointer")
	}
	err := json.Unmarshal(value, objPtr)
	return err
}

// FilterFunc test an object match the condition
type FilterFunc func(obj interface{}) bool

// decodeList decodes a list of values into a list of objects, with resource version set to corresponding rev.
// On success, ListPtr would be set to the list of objects.
func decodeList(elems []*elemForDecode, filter FilterFunc, ListPtr interface{}) error {
	v, err := EnforcePtr(ListPtr)
	if err != nil || v.Kind() != reflect.Slice {
		panic("need ptr to slice")
	}
	for _, elem := range elems {
		obj := reflect.New(v.Type().Elem()).Interface()
		err := decode(elem.data, obj)
		if err != nil {
			return err
		}
		// being unable to set the version does not prevent the object from being extracted
		if filter(obj) {
			v.Set(reflect.Append(v, reflect.ValueOf(obj).Elem()))
		}
	}
	return nil
}

func notFound(key string) clientv3.Cmp {
	return clientv3.Compare(clientv3.ModRevision(key), "=", 0)
}

// SetZeroValue would set the object of objPtr to zero value of its type.
func SetZeroValue(objPtr interface{}) error {
	v, err := EnforcePtr(objPtr)
	if err != nil {
		return err
	}
	v.Set(reflect.Zero(v.Type()))
	return nil
}
