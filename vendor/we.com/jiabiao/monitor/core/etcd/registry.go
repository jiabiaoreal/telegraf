package etcd

import (
	"path/filepath"

	"we.com/jiabiao/monitor/core/types"
)

const (
	// HostBasePrefix  used when create generic.Interface
	HostBasePrefix      = "/mnt/hosts"
	hostInfoPrex        = "info"
	hostStatPrex        = "stat"
	hostKeyPrex         = "keys"
	hostConfigPrex      = "config"
	hostReplicaSpecPrex = "replicaSpec"
	hostEventPrex       = "event" // restart, stop, start, a instances
)

// GetHostInfoPath return etcd relative path
func GetHostInfoPath(env types.ENV, hostid types.UUID) string {
	return filepath.Join(hostInfoPrex, string(env), string(hostid))
}

// GetHostStatPath return etcd relative path
func GetHostStatPath(env types.ENV, hostid types.UUID) string {
	return filepath.Join(hostStatPrex, string(env), string(hostid))
}

// GetHostKeyPath return etcd relative path
func GetHostKeyPath(env types.ENV, hostid types.UUID) string {
	return filepath.Join(hostKeyPrex, string(env), string(hostid))
}

// GetHostEventPath return etcd relative path
func GetHostEventPath(env types.ENV, hostid types.UUID) string {
	return filepath.Join(hostEventPrex, string(env), string(hostid))
}

// GetHostReplicaSpecPath return etcd relative path
func GetHostReplicaSpecPath(env types.ENV, hostid types.UUID) string {
	return filepath.Join(hostReplicaSpecPrex, string(env), string(hostid))
}

// GetHostReplicaSpecPrefix return etcd relative path
func GetHostReplicaSpecPrefix(env types.ENV) string {
	return filepath.Join(hostReplicaSpecPrex, string(env))
}

const (
	DeployBasePrefix = "/mnt/deploy"
	routePrex        = "route"
	configPrex       = "config" //  this is the transformed config, raw configs read from file store with project
	instancesPrefix  = "instances"
	expectPrex       = "expect"
)

// GetInstancePath return etcd relative path
func GetInstancePath(env types.ENV, typ types.ProjectType, cluster types.UUID, nodeid types.UUID) string {
	return filepath.Join(instancesPrefix, string(env), string(typ), string(cluster), string(nodeid))
}

// GetInstancePathPrefix return etcd relative path
func GetInstancePathPrefix() string {
	return instancesPrefix
}

// GetInstancePathPrefixOfEnv  return etcd relative path
func GetInstancePathPrefixOfEnv(env types.ENV) string {
	return filepath.Join(instancesPrefix, string(env))
}

// GetInstancePathPrefixOfType  return etcd relative path
func GetInstancePathPrefixOfType(env types.ENV, typ types.ProjectType) string {
	return filepath.Join(instancesPrefix, string(env), string(typ))
}

// GetInstancePathPrefixOfCluster return etcd relative path of project cluster
func GetInstancePathPrefixOfCluster(env types.ENV, typ types.ProjectType, cluster types.UUID) string {
	return filepath.Join(instancesPrefix, string(env), string(typ), string(cluster))
}

const (
	// stores type specific config  of a project, this is the config, almost the same with config files
	ProjectBasePrefix    = "/mnt/project/"
	projectInfoPrefix    = "info"
	projectDeployPrefix  = "deploy"
	projectProbePrefix   = "probe"
	projectVersionPrefix = "version"
)

// GetClusterInfoPath returns etcd relative  path of  cluster of type typ
func GetClusterInfoPath(env types.ENV, typ types.ProjectType, cluster types.UUID) string {
	return filepath.Join(projectInfoPrefix, string(env), string(typ), string(cluster))
}

// GetClusterInfoTypePrefix returns etcd relative  path for projectinfo of ProjectType typ
func GetClusterInfoTypePrefix(env types.ENV, typ types.ProjectType) string {
	return filepath.Join(projectInfoPrefix, string(env), string(typ))
}

// GetClusterDeploy returns  etcd relative  path  for deoploy config of project cluster
func GetClusterDeploy(env types.ENV, typ types.ProjectType, cluster types.UUID) string {
	return filepath.Join(projectDeployPrefix, string(env), string(typ), string(cluster))
}

// GetClusterDeployPrefixOfType  returns  etcd relative  path  for deoploy config of projectType typ
func GetClusterDeployPrefixOfType(env types.ENV, typ types.ProjectType) string {
	return filepath.Join(projectDeployPrefix, string(env), string(typ))
}

// GetClusterProbe returns  etcd relative  path  for probe config of  project cluster
func GetClusterProbe(env types.ENV, typ types.ProjectType, cluster types.UUID) string {
	return filepath.Join(projectProbePrefix, string(env), string(typ), string(cluster))
}

// GetClusterVersionInfoPrefix  returns etcd version info prefix of type env
func GetClusterVersionInfoPrefix(env types.ENV) string {
	return filepath.Join(projectVersionPrefix, string(env))
}

// GetClusterVersionInfoPrefixOfCluster  returns etcd version info prefix of type env
func GetClusterVersionInfoPrefixOfCluster(env types.ENV, typ types.ProjectType, cluster types.UUID) string {
	return filepath.Join(projectVersionPrefix, string(env), string(typ), string(cluster))
}

// GetClusterVersionInfoPrefixOfType  returns etcd version info prefix of type env
func GetClusterVersionInfoPrefixOfType(env types.ENV, typ types.ProjectType) string {
	return filepath.Join(projectVersionPrefix, string(env), string(typ))
}
