package types

import (
	"fmt"
	"time"

	"github.com/hashicorp/go-multierror"
)

// HostKeyConf ssh key config
type HostKeyConf struct {
	User       string `json:"user"`
	Password   string `json:"password"`
	PrivateKey string `json:"privateKey"`
	Port       int    `json:"port"`
	Agent      bool   `json:"aget"`
	Timeout    string `json:"timeout"`
	ScriptPath string `json:"scriptPath"`

	BastionUser       string            `json:"bastionUser,omitempty"`
	BastionPassword   string            `json:"bastionPassword,omitempty"`
	BastionPrivateKey string            `json:"bastionPrivateKey,omitempty"`
	BastionHost       string            `json:"bastionHost,omitempty"`
	BastionPort       int               `json:"bastionPort,omitempty"`
	ENV               ENV               `json:"env"`
	ConnInfo          map[string]string `json:"-"`
}

// GetConnectInfo tanslate fields to a map
func (sc *HostKeyConf) GetConnectInfo() map[string]string {
	if sc.ConnInfo != nil && len(sc.ConnInfo) > 0 {
		return sc.ConnInfo
	}

	cm := map[string]string{}
	cm["user"] = sc.User
	cm["port"] = fmt.Sprintf("%d", sc.Port)
	cm["password"] = sc.Password
	cm["privateKey"] = sc.PrivateKey
	cm["timeout"] = sc.Timeout
	cm["scriptPath"] = sc.ScriptPath
	cm["agent"] = fmt.Sprintf("%v", sc.Agent)

	cm["bastionUser"] = sc.BastionUser
	cm["bastionPassword"] = sc.BastionPassword
	cm["bastionPrivateKey"] = sc.BastionPrivateKey
	cm["bastionHost"] = sc.BastionHost
	cm["bastionPort"] = fmt.Sprintf("%d", sc.BastionPort)

	sc.ConnInfo = cm

	return cm
}

// HostConfig  host config infos
type HostConfig struct {
	HostID   string            `json:"hostID,omitempty"`
	HostName string            `json:"hostName,omitempty"`
	IPs      map[string]string `json:"ips,omitempty"`
	HostKey  string            `json:"hostKey,omitempty"`

	ENV    ENV               `json:"env,omitempty"`
	Labels map[string]string `json:"labels"` // labels are used as selectors

	InputPlugins  []string          `json:"inputPlugins,omitempty"`
	OutputPlugins []string          `json:"outputPlugins,omitempty"`
	ReportTags    map[string]string `json:"reportTags,omitempty"` // tags are use as agent global tags when report metrics
}

// HostCondition  host condition happing
type HostCondition string

const (
	// HostMemoryShortage  host short of memory
	HostMemoryShortage HostCondition = "memoryShortage"
	// HostLoadHigh  load high
	HostLoadHigh HostCondition = "loadHigh"
	// HostNetWidthHigh bandwidth used to much
	HostNetWidthHigh HostCondition = "netWidthHigh"
	// HostThreadHigh host short of pid resource
	HostThreadHigh HostCondition = "threadsToMuch"
	// HostDiskShortage  disk  space
	HostDiskShortage HostCondition = "diskShortage"
	// HostMetaInfoChanged  host meta info changed
	HostMetaInfoChanged HostCondition = "metaInfoChanged"
	// HostHealthy  host is healthy
	HostHealthy HostCondition = "healthy"
)

// HostEvent host status changes
type HostEvent struct {
	HostID            UUID
	PreviousCondition HostCondition
	CurrentCondition  HostCondition
	Msg               string
}

// HostInfo static info of this host, which do not update  frequently
type HostInfo struct {
	HostID   UUID   `json:"uuid,omitempty"`
	HostName string `json:"hostname,omitempty"`
	ENV      ENV    `json:"env,omitempty"`

	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"properties,omitempty"`

	HostKeyConf UUID `json:"hostKeyID,omitempty"`

	NumOfCPUs int                  `json:"numOfCPUs,omitempty"`
	Memory    uint64               `json:"memory,omitempty"`
	SwapSize  uint64               `json:"swap_size,omitempty"`
	Disk      map[string]*DiskStat `json:"disk,omitempty"`
	IPs       map[string]string    `json:"ips,omitempty"`

	UpdateTime time.Time     `json:"updateTime,omitempty"`
	Condition  HostCondition `json:"condition,omitempty"`
	Msg        string        `json:"msg,omitempty"`

	HostKey HostKeyConf `json:"-,omitempty"`
}

// Validate  check if the hi is valid,  if not err is not nil
func (hi HostInfo) Validate() error {
	var err *multierror.Error

	if hi.HostName != "" && !hostrex.MatchString(hi.HostName) {
		err = multierror.Append(err, fmt.Errorf("host format not correct"))
	}

	if hi.HostKeyConf == "" {
		err = multierror.Append(err, fmt.Errorf("hostkeyConf is empty, %v", hi.HostID))
	}

	if hi.ENV == "" {
		err = multierror.Append(err, fmt.Errorf("env cannot be empty"))
	}

	return err.ErrorOrNil()
}

const (
	cpuResource = uint64(1024 * 1024 * 1024)
	gb          = 1024 * 1024 * 1024 / 8
)

// GetResource get total resource in the form of DeployResource
func (hi HostInfo) GetResource() DeployResource {
	diskSpace := uint64(0)
	for _, v := range hi.Disk {
		diskSpace += v.Totoal
	}
	ret := DeployResource{
		Memory:     hi.Memory,
		CPU:        uint64(hi.NumOfCPUs) * cpuResource,
		DiskSpace:  diskSpace,
		NetworkIn:  gb,
		NetworkOut: gb,
	}

	return ret
}

// HostStatus runtime host status
// Tags are set by users, annotations are used internally
type HostStatus struct {
	HostID     UUID      `json:"hostID"`
	HostName   string    `json:"hostname"`
	IP         string    `json:"ip"`
	UpdateTime time.Time `json:"updateTime"`

	Load1    float64 `json:"load1"`
	Load5    float64 `json:"load5"`
	Load15   float64 `json:"load15"`
	CPUUsage float64 `json:"cpuUsage"`

	FreeMemory   uint64 `json:"freeMemory"`
	CachedMemory uint64 `json:"cachedMemory"`
	FreeSwap     uint64 `json:"freeSwap"`
	Sin          uint64 `json:"sin"`
	Sout         uint64 `json:"out"`

	BandWidthUsage map[string]NetIOStat `json:"netStat"`

	DiskStat map[string]DiskStat `json:"diskStat"`

	NumOfThreads   int `json:"numOfThreads"`
	NumofProcesses int `json:"numOfProcesses"`
}

// DiskStat  disk partition stat
type DiskStat struct {
	Devices    string `json:"devices,omitempty"`
	Mountpoint string `json:"mountpoint,omitempty"`
	Totoal     uint64 `json:"totoal,omitempty"`
	Free       uint64 `json:"free,omitempty"`
	ReadSpeed  uint64 `json:"readSpeed,omitempty"`
	WriteSpeed uint64 `json:"writeSpeed,omitempty"`
	ReadCount  uint64 `json:"readCount,omitempty"`
	WriteCount uint64 `json:"writeCount,omitempty"`
}

// NetIOStat nic net stat
type NetIOStat struct {
	Name        string `json:"name"`        // interface name
	BytesSent   uint64 `json:"bytesSent"`   // number of bytes sent  per second
	BytesRecv   uint64 `json:"bytesRecv"`   // number of bytes received  per second
	PacketsSent uint64 `json:"packetsSent"` // number of packets sent  per second
	PacketsRecv uint64 `json:"packetsRecv"` // number of packets received  per second
}
