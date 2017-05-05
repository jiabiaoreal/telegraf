package types

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	radix "github.com/armon/go-radix"
	"github.com/golang/glog"
	"github.com/golang/sync/syncmap"
	multierror "github.com/hashicorp/go-multierror"
)

// MonitorType monitor type: java, es, nginx etc.
type MonitorType ProjectType

// ClusterReplicaSpec  expected  number of processes running on this host
type ClusterReplicaSpec struct {
	Type ProjectType `json:"type,omitempty"`
	// ClusterName: which  cluster this  proccess should belong to
	// for java: this should of the form: project:bin:version
	// version should be add here, for there may be multiple version running
	// for redis: this may should into port number if multi instances of the same cluster running  on this host
	// so: this field should be able to uniquely  identify a server
	ClusterName UUID `json:"clusterName,omitempty"`
	// Version may be not needed:
	Version      string `json:"version,omitempty"`
	InstancesNum int    `json:"instancesNum,omitempty"`
}

const (
	// VersionAll match all versions
	VersionAll = "_all"

	// VersionLatest lastest version
	VersionLatest = "_latest"
	// VersionNone no version
	VersionNone = ""
)

// TypeReplicaSpec is a map
// key is clustername, value is *clusterReplicaSpec
// at the beginning, we use a map to hold  clusterSpec
// but since version num is mutable,  and it's hard  find
// a list of match ClusterSpec for an unversion config
// so, now we use a radis-tree, to find prefix matches
type TypeReplicaSpec struct {
	// lock protect update for tree
	lock sync.RWMutex
	tree *radix.Tree
}

// GetType get project Type
func (trs *TypeReplicaSpec) GetType() ProjectType {
	var typ ProjectType
	trs.Range(func(c UUID, v *ClusterReplicaSpec) bool {
		typ = v.Type
		return false
	})

	return typ
}

// Add add a spec to and exists  specs, InstanceNum will be the sum of two
func (trs *TypeReplicaSpec) Add(crs ClusterReplicaSpec) {
	glog.V(15).Infof("add cluster: %v", crs)
	cluster := fmt.Sprintf("%v%v%v", crs.ClusterName, FieldSperator, crs.Version)

	trs.lock.Lock()
	defer trs.lock.Unlock()

	v, ok := trs.tree.Get(cluster)
	if !ok {
		trs.tree.Insert(cluster, &crs)
		return
	}

	old := v.(*ClusterReplicaSpec)

	crs.InstancesNum = old.InstancesNum + crs.InstancesNum

	trs.tree.Insert(cluster, &crs)
}

// Update reset spec of cluster to crs
func (trs *TypeReplicaSpec) Update(crs ClusterReplicaSpec) {
	cluster := fmt.Sprintf("%v%v%v", crs.ClusterName, FieldSperator, crs.Version)

	trs.lock.Lock()
	defer trs.lock.Unlock()
	trs.tree.Insert(cluster, &crs)
}

// TypeRangeFunc handler func:
// return ture, means continue, false:  terminated
type TypeRangeFunc func(cluster UUID, value *ClusterReplicaSpec) bool

// Range over the cluster map
func (trs *TypeReplicaSpec) Range(f TypeRangeFunc) {
	trs.tree.Walk(func(k string, v interface{}) bool {
		value := v.(*ClusterReplicaSpec)

		// in order to keep the mean of reture value align to HostReplicaSpec.Range
		// return ture, means continue, false:  terminated

		return !f(value.ClusterName, value)
	})
}

// Get return  spec of cluster, if not exist return nil
// if cluster has no version info, we walkPrefix of the cluster
func (trs *TypeReplicaSpec) Get(cluster UUID, version string) *ClusterReplicaSpec {
	trs.lock.RLock()
	defer trs.lock.RUnlock()

	wtype := "all"
	switch version {
	case VersionAll:
		wtype = "all"
	case VersionLatest:
		wtype = "latest"
	case VersionNone:
		fallthrough
	default:
		v, ok := trs.tree.Get(fmt.Sprintf("%v%v%v", cluster, FieldSperator, version))
		if !ok {
			return nil
		}
		return v.(*ClusterReplicaSpec)
	}

	// all or  latest
	prefix := string(cluster)

	var ptype ProjectType

	latest := ""
	sum := 0
	trs.tree.WalkPrefix(prefix, func(c string, v interface{}) bool {
		value := v.(*ClusterReplicaSpec)
		switch wtype {
		case "all":
			sum += value.InstancesNum
		case "latest":
			if VersionCmp(c, latest) > 0 {
				sum = value.InstancesNum
				latest = c
			}
		}
		ptype = value.Type

		return true
	})

	return &ClusterReplicaSpec{
		Type:         ptype,
		ClusterName:  cluster,
		Version:      latest,
		InstancesNum: sum,
	}
}

// VersionCmp  compare two version string
func VersionCmp(a, b string) int {
	if len(a) > len(b) {
		return 1
	}
	if len(a) < len(b) {
		return -1
	}

	if a > b {
		return 1
	}
	if a < b {
		return -1
	}
	return 0
}

func (trs *TypeReplicaSpec) String() string {
	ret := ""
	trs.Range(func(cluster UUID, v *ClusterReplicaSpec) bool {
		ret += fmt.Sprintf("\n%v: v%v, %v", v.ClusterName, v.Version, v.InstancesNum)
		return true
	})
	return strings.TrimLeft(ret, "\n")
}

// NewTypeReplicaSpec  return a *TYpeReplicaSpec
func NewTypeReplicaSpec() *TypeReplicaSpec {
	return &TypeReplicaSpec{
		tree: radix.New(),
	}
}

// HostReplicaSpec cluster of type  suppose|actully running on this host
// todo(nathon.jia): change to syncmap.Map, so we don't need rwmutx to protect it
type HostReplicaSpec struct {
	// data map[ProjectType]map[string]*TypeReplicaSpec
	data syncmap.Map
}

// TypeList return a list of stored types
func (hrs *HostReplicaSpec) TypeList() []ProjectType {
	ret := []ProjectType{}

	hrs.data.Range(func(k, v interface{}) bool {
		typ := k.(ProjectType)
		ret = append(ret, typ)
		return true
	})

	return ret
}

// Get return Typespec of typ, if not exist, returns a new Instances and false
func (hrs *HostReplicaSpec) Get(typ ProjectType) (*TypeReplicaSpec, bool) {
	trs, ok := hrs.data.Load(typ)
	if ok {
		return trs.(*TypeReplicaSpec), true
	}

	return &TypeReplicaSpec{tree: radix.New()}, false
}

// Set set spec to typ to trs
func (hrs *HostReplicaSpec) Set(typ ProjectType, trs *TypeReplicaSpec) {
	hrs.data.Store(typ, trs)
}

// HostRangeFunc handler func:
// return ture, means continue, false:  terminated
type HostRangeFunc func(k ProjectType, v *TypeReplicaSpec) bool

// Range range over projectType of this hostReplicatSpec
func (hrs *HostReplicaSpec) Range(f HostRangeFunc) {
	hrs.data.Range(func(k, v interface{}) bool {
		typ := k.(ProjectType)
		value := v.(*TypeReplicaSpec)
		return f(typ, value)
	})
}

// AddCluserSpec adds crs to hrs
func (hrs *HostReplicaSpec) AddCluserSpec(crs ClusterReplicaSpec) {
	typ := crs.Type
	trs, ok := hrs.Get(typ)
	if !ok {
		hrs.data.Store(typ, trs)
	}

	trs.Add(crs)
}

// AddTypeSpec adds cluster spec in trs to hrs
func (hrs *HostReplicaSpec) AddTypeSpec(trs *TypeReplicaSpec) {
	trs.Range(func(cluster UUID, value *ClusterReplicaSpec) bool {
		hrs.AddCluserSpec(*value)
		return true
	})
}

func (hrs *HostReplicaSpec) String() string {
	ret := ""
	hrs.Range(func(k ProjectType, v *TypeReplicaSpec) bool {
		ret += "\n" + fmt.Sprintf("%v: %+v", k, v)
		return true
	})
	return strings.TrimLeft(ret, "\n")
}

// UnmarshalJSON  implements marshal interface
func (hrs *HostReplicaSpec) UnmarshalJSON(data []byte) error {
	type configEntity map[ProjectType]map[string]interface{}

	var ce = configEntity{}

	if err := json.Unmarshal(data, &ce); err != nil {
		err = fmt.Errorf("Unmarshal expectation error: %s", err)
		glog.Warningf("%s", err)
		return err
	}

	var merr *multierror.Error
	for k, v := range ce {
		typeValue, ok := hrs.Get(k)
		if !ok {
			hrs.Set(k, typeValue)
		}

		if err := _parseUnmarshalData(v, typeValue, "", k); err != nil {
			merr = multierror.Append(merr, err)
		}
	}
	return merr.ErrorOrNil()
}

// MarshalJSON implement json Marshaler interface
func (hrs *HostReplicaSpec) MarshalJSON() ([]byte, error) {
	if hrs == nil {
		return []byte{}, nil
	}

	ret := map[ProjectType]map[UUID]*ClusterReplicaSpec{}

	hrs.Range(func(pt ProjectType, v *TypeReplicaSpec) bool {
		ret[pt] = map[UUID]*ClusterReplicaSpec{}
		tmap := ret[pt]
		v.Range(func(c UUID, cv *ClusterReplicaSpec) bool {
			key := UUID(fmt.Sprintf("%v%v%v", cv.ClusterName, FieldSperator, cv.Version))
			tmap[key] = cv
			return true
		})
		return true
	})

	return json.Marshal(ret)
}

// data is config of a cluster
func _parseUnmarshalData(data map[string]interface{}, typeValue *TypeReplicaSpec, prefixKey string, typ ProjectType) error {
	if len(data) == 0 {
		return nil
	}

	type tmpClusterSpec struct {
		ClusterReplicaSpec

		XXX map[string]interface{} `json:"-"`
	}

	var merr *multierror.Error

	for cluster, value := range data {
		tmpKey := fmt.Sprintf("%v%v%v", prefixKey, FieldSperator, cluster)
		tmpKey = strings.TrimLeft(tmpKey, FieldSperator)
		idx := strings.LastIndex(tmpKey, FieldSperator)
		version := tmpKey[idx+1:]
		key := UUID(tmpKey[:idx])
		switch av := value.(type) {
		case float64:
			if cfg := typeValue.Get(key, version); cfg != nil {
				glog.Warningf("config specify more than one time for %v, old: %v, new: %v", key, cfg, value)
				merr = multierror.Append(merr, fmt.Errorf("cluster %s configged for more than one time", key))
				continue
			}
			typeValue.Add(ClusterReplicaSpec{
				Type:         typ,
				InstancesNum: int(av + 0.1), // When converting a floating-point number to an integer, the fraction is discarded
				ClusterName:  UUID(key),
				Version:      version,
			})
		case map[string]interface{}:
			data, _ := json.Marshal(av)
			cv := tmpClusterSpec{
				ClusterReplicaSpec: ClusterReplicaSpec{
					Type:        typ,
					ClusterName: UUID(key),
					Version:     version,
				},
			}
			err := json.Unmarshal(data, &cv)
			if err != nil || len(cv.XXX) > 0 {
				err = _parseUnmarshalData(av, typeValue, string(key), typ)
				if err != nil {
					merr = multierror.Append(merr, err)
				}
			} else {
				typeValue.Add(cv.ClusterReplicaSpec)
			}
		default:
			err := fmt.Errorf("unknown key:value: (%v, %v)", key, av)
			glog.Warningf("%v", err)
			merr = multierror.Append(merr, err)
		}
	}

	return merr.ErrorOrNil()
}

// MergeHostReplicaSpec merge mulitple config together
func MergeHostReplicaSpec(specs ...*HostReplicaSpec) *HostReplicaSpec {
	ret := HostReplicaSpec{}

	for _, spec := range specs {
		if spec == nil {
			continue
		}
		spec.Range(func(p ProjectType, typValue *TypeReplicaSpec) bool {
			typValue.Range(func(c UUID, value *ClusterReplicaSpec) bool {
				ret.AddCluserSpec(*value)
				return true
			})
			return true
		})
	}

	return &ret
}

//DiffHostReplicaSpec diff  a and b: return lack and residual,
func DiffHostReplicaSpec(a, b *HostReplicaSpec) (lack, residual *HostReplicaSpec) {
	lack = &HostReplicaSpec{}
	residual = &HostReplicaSpec{}

	if a == nil {
		a = &HostReplicaSpec{data: syncmap.Map{}}
	}
	if b == nil {
		b = &HostReplicaSpec{data: syncmap.Map{}}
	}

	a.Range(func(typ ProjectType, typValue *TypeReplicaSpec) bool {
		dstTypValue, ok := b.Get(typ)
		if !ok {
			typValue.Range(func(c UUID, value *ClusterReplicaSpec) bool {
				residual.AddCluserSpec(*value)
				return true
			})
			return true
		}

		typValue.Range(func(c UUID, value *ClusterReplicaSpec) bool {
			dstClusterValue := dstTypValue.Get(c, value.Version)
			if dstClusterValue == nil {
				residual.AddCluserSpec(*value)
				return true
			}

			newClusterValue := *value
			newClusterValue.InstancesNum = value.InstancesNum - dstClusterValue.InstancesNum
			if newClusterValue.InstancesNum > 0 {
				residual.AddCluserSpec(newClusterValue)
			} else if newClusterValue.InstancesNum < 0 {
				newClusterValue.InstancesNum = -newClusterValue.InstancesNum
				lack.AddCluserSpec(newClusterValue)
			}
			return true
		})
		return true
	})

	b.Range(func(typ ProjectType, dstTypValue *TypeReplicaSpec) bool {
		typValue, ok := a.Get(typ)
		if !ok {
			dstTypValue.Range(func(c UUID, value *ClusterReplicaSpec) bool {
				lack.AddCluserSpec(*value)
				return true
			})
			return true
		}

		dstTypValue.Range(func(c UUID, value *ClusterReplicaSpec) bool {
			if tmp := typValue.Get(c, value.Version); tmp == nil {
				lack.AddCluserSpec(*value)
			}
			return true
		})

		return true
	})
	if len(lack.TypeList()) == 0 {
		lack = nil
	}
	if len(residual.TypeList()) == 0 {
		residual = nil
	}
	return
}

//DiffHostReplicaSpecIgnoreVersion diff versioned and unversioned: return lack and residual of unversioned
func DiffHostReplicaSpecIgnoreVersion(a, b *HostReplicaSpec) (lack, residual *HostReplicaSpec) {
	lack = &HostReplicaSpec{}
	residual = &HostReplicaSpec{}

	if a == nil {
		a = &HostReplicaSpec{data: syncmap.Map{}}
	}
	if b == nil {
		b = &HostReplicaSpec{data: syncmap.Map{}}
	}

	clusterMap := map[UUID]struct{}{}

	a.Range(func(typ ProjectType, typValue *TypeReplicaSpec) bool {
		dstTypValue, hasType := b.Get(typ)

		typValue.Range(func(c UUID, value *ClusterReplicaSpec) bool {
			allCluster := UUID(fmt.Sprintf("%v%v%v", c, FieldSperator, VersionAll))
			stripedCluster := UUID(fmt.Sprintf("%v%v%v", c, FieldSperator, VersionNone))
			if _, ok := clusterMap[allCluster]; ok {
				return true
			}
			clusterMap[allCluster] = struct{}{}

			value = typValue.Get(c, VersionAll)
			value.ClusterName = stripedCluster
			if !hasType {
				residual.AddCluserSpec(*value)
				return true
			}

			dstClusterValue := dstTypValue.Get(c, VersionAll)
			if dstClusterValue == nil {
				residual.AddCluserSpec(*value)
				return true
			}

			newClusterValue := *value
			newClusterValue.InstancesNum = value.InstancesNum - dstClusterValue.InstancesNum
			if newClusterValue.InstancesNum > 0 {
				residual.AddCluserSpec(newClusterValue)
			} else if newClusterValue.InstancesNum < 0 {
				newClusterValue.InstancesNum = -newClusterValue.InstancesNum
				lack.AddCluserSpec(newClusterValue)
			}
			return true
		})
		return true
	})

	b.Range(func(typ ProjectType, dstTypValue *TypeReplicaSpec) bool {
		typValue, hasType := a.Get(typ)

		dstTypValue.Range(func(c UUID, value *ClusterReplicaSpec) bool {
			allCluster := UUID(fmt.Sprintf("%v%v%v", c, FieldSperator, VersionAll))
			stripedCluster := UUID(fmt.Sprintf("%v%v%v", c, FieldSperator, VersionNone))
			if _, ok := clusterMap[allCluster]; ok {
				return true
			}
			clusterMap[allCluster] = struct{}{}

			value = dstTypValue.Get(c, VersionAll)
			value.ClusterName = stripedCluster

			if !hasType {
				lack.AddCluserSpec(*value)
				return true
			}

			if tmp := typValue.Get(c, VersionAll); tmp == nil {
				lack.AddCluserSpec(*value)
			}
			return true
		})
		return true
	})

	if len(lack.TypeList()) == 0 {
		lack = nil
	}
	if len(residual.TypeList()) == 0 {
		residual = nil
	}
	return
}

// ParseClusterVersion parse raw to cluster and verion
// if raw is "crm-server:bin:123" -> cluster: crm-server:bin, version: 123
// if raw if "crm-server:bin:" -> cluster: crm-server:bin,  version:
// if raw if "crm-server" -> cluster: crm-server,  version:
// if raw if "" -> cluster: , version:
func ParseClusterVersion(raw string) (cluster UUID, version string) {
	idx := strings.LastIndex(raw, FieldSperator)
	return UUID(raw[:idx]), raw[idx+1:]
}
