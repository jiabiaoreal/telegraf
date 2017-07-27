// +build linux

package process

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/golang/glog"
	"we.com/jiabiao/monitor/core/types"
)

const (
	root = "/proc"
)

// ListenPortsOfPid listening port of pid  tcp/ipv4
func ListenPortsOfPid(pid int) ([]types.Addr, error) {
	var ret []types.Addr
	dir := filepath.Join(root, strconv.Itoa(pid), "fd")
	f, err := os.Open(dir)
	if err != nil {
		return ret, nil
	}
	defer f.Close()

	glog.V(10).Infof("start to list listenPorts")
	inodeMap, err := getListenPort()
	if err != nil {
		return ret, err
	}
	glog.V(15).Infof("listen ports: %v", inodeMap)

	var ports []types.Addr
	walkFunc := func(path string, fd os.FileInfo, err error) error {
		if err != nil {
			// We should continue processing other directories/files
			return nil
		}
		if strings.HasSuffix(path, "fd") {
			return nil
		}
		inode, err := os.Readlink(path)
		if err != nil {
			glog.Infof("walk %v: %v", path, err)
			return nil
		}
		if !strings.HasPrefix(inode, "socket:[") {
			return nil
		}

		// the process is using a socket
		l := len(inode)
		inode = inode[8 : l-1]
		addr, ok := inodeMap[inode]
		if !ok {
			return nil
		}

		ports = append(ports, addr)
		return nil
	}

	glog.V(10).Infof("start to walt dir: %v", dir)
	if err = filepath.Walk(dir, walkFunc); err != nil {
		return nil, err
	}
	glog.V(10).Infof("walk dir end: %v", ports)

	// if pid, and ppid both listen the same port, then
	// we remove this port from child listen ports, if pid is 0, or 1, skip
	ppid, _ := getPPid(pid)
	glog.V(10).Infof("pid, ppid: %v, %v", pid, ppid)
	if ppid == 0 || ppid == 1 {
		return ports, nil
	}

	childPorts := ports
	ret = nil

	pdir := filepath.Join(root, strconv.Itoa(ppid), "fd")
	if err = filepath.Walk(pdir, walkFunc); err != nil {
		glog.Warning("walk ppid proc dir: %v", err)
		return childPorts, nil
	}

outer:
	for _, cport := range childPorts {
		for _, pport := range ports {
			if cport == pport {
				continue outer
			}
		}

		ret = append(ret, cport)
	}

	return ret, nil
}

func getListenPort() (map[string]types.Addr, error) {
	file := filepath.Join(root, "net", "tcp")
	freader, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer freader.Close()

	reader := bufio.NewReader(freader)
	// skip first line
	reader.ReadString('\n')

	var ret = map[string]types.Addr{}

	var noneListPortCount int
	// skip first line
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			glog.Infof("read /proc/net/tcp: %v", err)
			break
		}
		l := strings.Fields(line)
		if len(l) < 10 {
			continue
		}
		// status is not listen
		if l[3] != "0A" {
			noneListPortCount++
			if noneListPortCount > 5 {
				break
			} else {
				continue
			}
		}

		noneListPortCount = 0

		laddr := l[1]
		inode := l[9]
		la, err := decodeAddress(laddr)
		if err != nil {
			continue
		}

		ret[inode] = la
	}

	return ret, nil
}

func getPPid(pid int) (int, error) {
	return fillFromStat(pid)
}

// decodeAddress decode addresse represents addr in proc/net/*
// ex:
// "0500000A:0016" -> "10.0.0.5", 22
// "0085002452100113070057A13F025401:0035" -> "2400:8500:1301:1052:a157:7:154:23f", 53
func decodeAddress(src string) (types.Addr, error) {
	t := strings.Split(src, ":")
	if len(t) != 2 {
		return types.Addr{}, fmt.Errorf("does not contain port, %s", src)
	}
	addr := t[0]
	port, err := strconv.ParseInt("0x"+t[1], 0, 64)
	if err != nil {
		return types.Addr{}, fmt.Errorf("invalid port, %s", src)
	}
	decoded, err := hex.DecodeString(addr)
	if err != nil {
		return types.Addr{}, fmt.Errorf("decode error, %s", err)
	}
	// Assumes this is little_endian
	ip := net.IP(reverse(decoded))

	return types.Addr{
		IP:   ip.String(),
		Port: int(port),
	}, nil
}

// reverse reverses array of bytes.
func reverse(s []byte) []byte {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
	return s
}

// http://students.mimuw.edu.pl/lxr/source/include/net/tcp_states.h
var tcpStatuses = map[string]string{
	"01": "ESTABLISHED",
	"02": "SYN_SENT",
	"03": "SYN_RECV",
	"04": "FIN_WAIT1",
	"05": "FIN_WAIT2",
	"06": "TIME_WAIT",
	"07": "CLOSE",
	"08": "CLOSE_WAIT",
	"09": "LAST_ACK",
	"0A": "LISTEN",
	"0B": "CLOSING",
}

func fillFromStat(pid int) (int, error) {
	statPath := filepath.Join(root, strconv.Itoa(pid), "stat")
	contents, err := ioutil.ReadFile(statPath)
	if err != nil {
		return 0, err
	}

	fields := strings.Fields(string(contents))

	i := 1
	for !strings.HasSuffix(fields[i], ")") {
		i++
	}
	ppid, err := strconv.ParseInt(fields[i+2], 10, 32)
	if err != nil {
		return 0, err
	}
	return int(ppid), nil
}
