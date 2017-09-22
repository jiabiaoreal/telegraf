package java

import (
	"encoding/json"
	"fmt"
	"io"

	"we.com/jiabiao/common/yaml"
	"we.com/jiabiao/monitor/core/types"
)

var (
	defaultSelector = map[string]string{
		"ptjava": "true",
	}
)

// DeployConfig  java deploy config
type DeployConfig struct {
	Project string `json:"project,omitempty"`

	// Image  is not versioned
	Image        string                   `json:"images,omitempty"`
	UpdatePolicy types.DeployUpdatePolicy `json:"updatePolicy,omitempty"`

	DeployDir        string                     `json:"deployDir,omitempty"`
	ResourceRequired *types.DeployResource      `json:"resourceRequired,omitempty"`
	Values           map[string]interface{}     `json:"values,omitempty"`
	Labels           map[string]string          `json:"selector,omitempty"`
	Bins             map[string]BinDeployConfig `json:"bins,omitempty"`
}

// BinDeployConfig bin deploy config
type BinDeployConfig struct {
	ServiceType      types.ServiceType      `json:"serviceTypes,omitempty"`
	Image            string                 `json:"images,omitempty"`
	Values           map[string]interface{} `json:"values,omitempty"`
	DeployDir        string                 `json:"deployDir,omitempty"`
	RestartPolicy    types.RestartPolicy    `json:"restartPolicy,omitempty"`
	ResourceRequired *types.DeployResource  `json:"resourceRequired,omitempty"`

	Labels        map[string]string `json:"selector,omitempty"`
	NumOfInstance int               `json:"numOfInstance,omitempty"`
}

// UnmarshalJSON implements json.Unmarshaler interface
func (dc *DeployConfig) UnmarshalJSON(data []byte) error {
	type plain DeployConfig

	if err := json.Unmarshal(data, (*plain)(dc)); err != nil {
		return err
	}

	if dc.ResourceRequired == nil {
		dc.ResourceRequired = &types.DeployResource{}
	}

	return nil
}

type tmpBinConfig struct {
	Labels        map[string]string `json:"selector,omitempty"`
	NumOfInstance map[string]int    `json:"numOfInstance"`
}

// UnmarshalJSON implements json encode interface
func (bdc *tmpBinConfig) UnmarshalJSON(data []byte) error {
	type tmp struct {
		Labels        map[string]string `json:"selector"`
		NumOfInstance interface{}       `json:"numOfInstance"`
	}

	t := &tmp{}
	if err := json.Unmarshal(data, t); err != nil {
		return err
	}

	bdc.NumOfInstance = map[string]int{}
	bdc.Labels = t.Labels
	switch ni := t.NumOfInstance.(type) {
	case float64:
		bdc.NumOfInstance[""] = int(ni + 0.1)
	case map[string]float64:
		for k, v := range ni {
			bdc.NumOfInstance[k] = int(v + 0.1)
		}
	case map[string]interface{}:
		for k, v := range ni {
			if fv, ok := v.(float64); ok {
				bdc.NumOfInstance[k] = int(fv + 0.1)
			} else {
				return fmt.Errorf("expect a number value got: %v", v)
			}

		}
	default:
		return fmt.Errorf("unknown values for filed NumOfInstance: %v", ni)
	}

	return nil
}

// ToCommonDeployConfig  convert to  types.DeployConfig
func (dc *DeployConfig) ToCommonDeployConfig() map[types.UUID]*types.DeployConfig {
	ret := map[types.UUID]*types.DeployConfig{}

	for bn, bdinfo := range dc.Bins {
		resRequired := bdinfo.ResourceRequired
		labels := map[string]string{}
		for k, v := range dc.Labels {
			labels[k] = v
		}
		for k, v := range bdinfo.Labels {
			labels[k] = v
		}

		clusterStr := fmt.Sprintf("%v%v%v", dc.Project, types.FieldSperator, bn)
		id := types.UUID(clusterStr)

		res := *dc.ResourceRequired
		if resRequired.Memory != 0 {
			res.Memory = resRequired.Memory
		}
		if resRequired.CPU != 0 {
			res.CPU = resRequired.CPU
		}
		if resRequired.NetworkIn != 0 {
			res.NetworkIn = resRequired.NetworkIn
		}
		if resRequired.NetworkOut != 0 {
			res.NetworkOut = resRequired.NetworkOut
		}

		if resRequired.DiskSpace != 0 {
			res.DiskSpace = resRequired.DiskSpace
		}

		image := dc.Image
		if bdinfo.Image != "" {
			image = bdinfo.Image
		}

		numInstance := 1
		if bdinfo.NumOfInstance > 0 {
			numInstance = bdinfo.NumOfInstance
		}

		values := mergeMapTo(dc.Values, bdinfo.Values)

		dcfg := types.DeployConfig{
			Type:          Type,
			Cluster:       id,
			NumOfInstance: numInstance,
			ServiceType:   bdinfo.ServiceType,

			Image:            image,
			UpdatePolicy:     dc.UpdatePolicy,
			RestartPolicy:    bdinfo.RestartPolicy,
			DeployDir:        dc.DeployDir,
			Values:           values,
			Labels:           labels,
			ResourceRequired: res,
		}

		if bdinfo.DeployDir != "" {
			dcfg.DeployDir = bdinfo.DeployDir
		}

		ret[id] = &dcfg
	}

	return ret
}

// Validate check if config is valid
func (dc *DeployConfig) Validate() error {
	if len(dc.Project) == 0 {
		return fmt.Errorf("project name should not be empty：%v", dc)
	}

	if len(dc.Bins) == 0 {
		return fmt.Errorf("%v: at least one bin should configed", dc.Project)
	}

	return nil
}

// LoadDeployConfig load deploy config from reader
func LoadDeployConfig(reader io.Reader) (*DeployConfig, error) {
	decoder := yaml.NewYAMLOrJSONDecoder(reader, 4)
	ret := DeployConfig{}

	err := decoder.Decode(&ret)
	if err != nil {
		return nil, err
	}
	return &ret, nil
}

func mergeMapTo(a map[string]interface{}, b map[string]interface{}) map[string]interface{} {
	// b is empty
	if len(b) == 0 {
		ret := map[string]interface{}{}
		for k, v := range a {
			ret[k] = v
		}
		return ret
	}

	for k, v := range a {
		ov, ok := b[k]
		if !ok {
			b[k] = v
			continue
		}

		switch mb := ov.(type) {
		case map[string]interface{}:
			switch ma := v.(type) {
			case map[string]interface{}:
				b[k] = mergeMapTo(ma, mb)
			}
		}

	}

	return b
}
