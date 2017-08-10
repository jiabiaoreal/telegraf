package java

import (
	"fmt"
	"io"

	multierror "github.com/hashicorp/go-multierror"
	"we.com/jiabiao/common/yaml"
	"we.com/jiabiao/monitor/core/types"
)

// ProbeInterface  a single  probe interface
type ProbeInterface struct {
	Name        string            `json:"name,omitempty"`
	Desc        string            `json:"desc,omitempty"`
	Data        string            `json:"data,omitempty"`
	Header      map[string]string `json:"header,omitempty"`
	Envs        []string          `json:"env,omitempty"`
	Matches     map[string]string `json:"matches,omitempty"`
	DontMatches map[string]string `json:"dontMatches,omitempty"`
}

// Validate test if a ProbeInterface is valid
func (pi *ProbeInterface) Validate() error {
	var err *multierror.Error

	if pi.Desc == "" {
		terr := fmt.Errorf("interface description cannot be nil")
		err = multierror.Append(err, terr)
	}

	if pi.Data == "" {
		terr := fmt.Errorf("interface data cannot be nil")
		err = multierror.Append(err, terr)
	}

	return err.ErrorOrNil()
}

// ProbeConfig  probe config of a project
type ProbeConfig struct {
	Project    string                               `json:"project,omitempty"`
	Interfaces map[string]map[string]ProbeInterface `json:"interfaces,omitempty"`
}

// Validate checks if Probeconfig is valid
func (pc *ProbeConfig) Validate() error {
	for _, bins := range pc.Interfaces {
		for name, i := range bins {
			if len(i.Matches) == 0 && len(i.DontMatches) == 0 {
				i.Matches = map[string]string{
					"_contains": "success",
				}
			}

			i.Name = name

			if err := i.Validate(); err != nil {
				return err
			}
		}
	}

	return nil
}

// GetProbeInterfaces return probeinterface of bin and env
func (pc *ProbeConfig) GetProbeInterfaces(env types.ENV, bin string) []*ProbeInterface {
	if pc == nil {
		return nil
	}

	binPIs, ok := pc.Interfaces[bin]
	if !ok {
		return nil
	}

	ret := []*ProbeInterface{}
	for _, v := range binPIs {
		if len(v.Envs) == 0 {
			cp := v
			ret = append(ret, &cp)
			continue
		}
		for _, e := range v.Envs {
			if e == string(env) {
				cp := v
				ret = append(ret, &cp)
			}
		}
	}
	return ret
}

// LoadProbeConfig load probconfig from a reader
func LoadProbeConfig(reader io.Reader) (*ProbeConfig, error) {
	decoder := yaml.NewYAMLOrJSONDecoder(reader, 4)
	ret := ProbeConfig{}

	err := decoder.Decode(&ret)
	if err != nil {
		return nil, err
	}
	return &ret, nil
}
