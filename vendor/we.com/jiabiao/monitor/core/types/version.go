package types

import (
	"encoding/json"

	"github.com/golang/glog"
)

// VersionInfo version info of cluster ignore version
type VersionInfo struct {
	Type     ProjectType
	Cluster  UUID   // clusterName without version
	backup   string // backup version number
	expected string // expected version number
}

// MarshalJSON  json.Marshaler interface
func (vi *VersionInfo) MarshalJSON() ([]byte, error) {
	type t struct {
		Type     ProjectType `json:"type,omitempty"`
		Cluster  UUID        `json:"cluster,omitempty"`  // clusterName without version
		Backup   string      `json:"backup,omitempty"`   // backup version number
		Expected string      `json:"expected,omitempty"` // expected version number
	}

	tmp := t{
		Type:     vi.Type,
		Cluster:  vi.Cluster,
		Backup:   vi.backup,
		Expected: vi.expected,
	}

	return json.Marshal(tmp)
}

// UnmarshalJSON  json.Unmarshaler interface
func (vi *VersionInfo) UnmarshalJSON(data []byte) error {

	type t struct {
		Type     ProjectType `json:"type,omitempty"`
		Cluster  UUID        `json:"cluster,omitempty"`  // clusterName without version
		Backup   string      `json:"backup,omitempty"`   // backup version number
		Expected string      `json:"expected,omitempty"` // expected version number
	}
	tmp := &t{}

	if err := json.Unmarshal(data, tmp); err != nil {
		return err
	}

	vi.Type = tmp.Type
	vi.Cluster = tmp.Cluster
	vi.backup = tmp.Backup
	vi.expected = tmp.Expected

	return nil
}

// AddVersion add a new version to verison info
// if version already saw or is None, just return
// if version is lager the all known versions, update latest
func (vi *VersionInfo) AddVersion(version string) {
	if version == VersionNone {
		return
	}

	vi.backup = version
}

// SetExpected set expect version to exp,
func (vi *VersionInfo) SetExpected(exp string) {
	if exp != VersionNone {
		glog.Warning("set expected version to None")
		return
	}
	vi.backup = vi.expected
	vi.expected = exp
}

// GetExpected if expect set return expect, else return latest
func (vi *VersionInfo) GetExpected() string {
	return vi.expected
}

// GetBackup get backup verion
func (vi *VersionInfo) GetBackup() string {
	return vi.backup
}

// NewVersionInfo create a new version info
func NewVersionInfo(typ ProjectType, cluster UUID, expected string, backup string) *VersionInfo {
	return &VersionInfo{
		Type:     typ,
		Cluster:  cluster,
		expected: expected,
		backup:   backup,
	}
}
