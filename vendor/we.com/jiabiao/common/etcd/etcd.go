package etcd

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"time"

	"github.com/coreos/etcd/clientv3"
	"github.com/coreos/etcd/pkg/tlsutil"
	"github.com/golang/glog"
	mytime "we.com/jiabiao/common/time"
	"we.com/jiabiao/common/yaml"
)

// Config is a clientv3.Config
type Config clientv3.Config

// EtcdConfig  read etcdconfig
type etcdConfig struct {
	// Endpoints is a list of URLs.
	Endpoints []string `json:"endpoints"`

	// AutoSyncInterval is the interval to update endpoints with its latest members.
	// 0 disables auto-sync. By default auto-sync is disabled.
	AutoSyncInterval mytime.Duration `json:"auto-sync-interval"`

	// DialTimeout is the timeout for failing to establish a connection.
	DialTimeout mytime.Duration `json:"dial-timeout"`

	// Username is a username for authentication.
	Username string `json:"username"`

	// Password is a password for authentication.
	Password string `json:"password"`

	// RejectOldCluster when set will refuse to create a client against an outdated cluster.
	RejectOldCluster bool `json:"reject-old-cluster"`

	// tls
	InsecureTransport     *bool  `json:"insecure-transport"`
	InsecureSkipTLSVerify bool   `json:"insecure-skip-tls-verify"`
	Certfile              string `json:"cert-file"`
	Keyfile               string `json:"key-file"`
	CAfile                string `json:"ca-file"`
}

// NewEtcdConfig get etcd config from this config
func NewEtcdConfig(filename string) (Config, error) {
	ret := clientv3.Config{}
	content, err := ioutil.ReadFile(filename)
	if err != nil {
		err := fmt.Errorf("error read etcd config file: %v", err)
		glog.Error(err.Error())
		return Config(ret), err
	}

	reader := bytes.NewReader(content)

	yc := etcdConfig{}

	decoder := yaml.NewYAMLOrJSONDecoder(reader, 4)
	err = decoder.Decode(&yc)
	if err != nil {
		err := fmt.Errorf("error parse etcd config: %v", err)
		glog.Error(err.Error())
		return Config(ret), err
	}

	glog.V(4).Infof("get etcd config %v", ret)

	new := clientv3.Config{
		Endpoints:        yc.Endpoints,
		AutoSyncInterval: time.Duration(yc.AutoSyncInterval),
		DialTimeout:      time.Duration(yc.DialTimeout),
		Username:         yc.Username,
		Password:         yc.Password,
		RejectOldCluster: yc.RejectOldCluster,
	}

	if yc.InsecureTransport == nil || *yc.InsecureTransport {
		return Config(new), nil
	}

	var (
		cert *tls.Certificate
		cp   *x509.CertPool
	)

	if yc.Certfile != "" && yc.Keyfile != "" {
		cert, err = tlsutil.NewCert(yc.Certfile, yc.Keyfile, nil)
		if err != nil {
			return Config(ret), err
		}
	}

	if yc.CAfile != "" {
		cp, err = tlsutil.NewCertPool([]string{yc.CAfile})
		if err != nil {
			return Config(ret), err
		}
	}

	tlscfg := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: yc.InsecureSkipTLSVerify,
		RootCAs:            cp,
	}
	if cert != nil {
		tlscfg.Certificates = []tls.Certificate{*cert}
	}
	new.TLS = tlscfg

	return Config(new), nil
}
