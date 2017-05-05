// +build !linux

package process

import "we.com/jiabiao/monitor/core/types"

// ListenPortsOfPid listening port of pid  tcp/ipv4
func ListenPortsOfPid(pid int) ([]types.Addr, error) {
	return []types.Addr{}, nil
}
