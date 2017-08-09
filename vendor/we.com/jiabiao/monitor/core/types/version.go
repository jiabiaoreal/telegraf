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
func (vi VersionInfo) MarshalJSON() ([]byte, error) {
	type t struct {
		Type     ProjectType `json:"type,omitempty"`
		Cluster  UUID        `json:"cluster,omitempty"`  // clusterName without version
		Versions []string    `json:"versions,omitempty"` // slice of versions, first is expected, second is backup
	}

	versions := []string{vi.expected, vi.backup}

	tmp := t{
		Type:     vi.Type,
		Cluster:  vi.Cluster,
		Versions: versions,
	}

	return json.Marshal(tmp)
}

// UnmarshalJSON  json.Unmarshaler interface
func (vi *VersionInfo) UnmarshalJSON(data []byte) error {

	type t struct {
		Type     ProjectType `json:"type,omitempty"`
		Cluster  UUID        `json:"cluster,omitempty"`  // clusterName without version
		Versions []string    `json:"versions,omitempty"` // slice of versions, first is expected, second is backup
	}
	tmp := &t{}

	if err := json.Unmarshal(data, tmp); err != nil {
		return err
	}

	// ensure len(tmp.Versions) ge 2
	for len(tmp.Versions) < 2 {
		tmp.Versions = append(tmp.Versions, "")
	}

	vi.Type = tmp.Type
	vi.Cluster = tmp.Cluster
	vi.expected = tmp.Versions[0]
	vi.backup = tmp.Versions[1]

	return nil
}

// AddVersion add a new version to verison info
// if version already saw or is None, just return
// if version is lager the all known versions, update latest
func (vi *VersionInfo) AddVersion(version string) {
	vi.backup = vi.expected
	vi.expected = version
}

// SetExpected set expect version to exp,
func (vi *VersionInfo) SetExpected(exp string) {
	if exp == VersionNone {
		glog.Warning("set expected version to None")
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
