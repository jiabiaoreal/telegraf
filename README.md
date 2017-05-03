# Telegraf [![Circle CI](https://circleci.com/gh/influxdata/telegraf.svg?style=svg)](https://circleci.com/gh/influxdata/telegraf) [![Docker pulls](https://img.shields.io/docker/pulls/library/telegraf.svg)](https://hub.docker.com/_/telegraf/)

Telegraf is an agent written in Go for collecting, processing, aggregating,
and writing metrics.

Design goals are to have a minimal memory footprint with a plugin system so
that developers in the community can easily add support for collecting metrics
from well known services (like Hadoop, Postgres, or Redis) and third party
APIs (like Mailchimp, AWS CloudWatch, or Google Analytics).

Telegraf is plugin-driven and has the concept of 4 distinct plugins:

1. [Input Plugins](#input-plugins) collect metrics from the system, services, or 3rd party APIs
2. [Processor Plugins](#processor-plugins) transform, decorate, and/or filter metrics
3. [Aggregator Plugins](#aggregator-plugins) create aggregate metrics (e.g. mean, min, max, quantiles, etc.)
4. [Output Plugins](#output-plugins) write metrics to various destinations

For more information on Processor and Aggregator plugins please [read this](./docs/AGGREGATORS_AND_PROCESSORS.md).

New plugins are designed to be easy to contribute,
we'll eagerly accept pull
requests and will manage the set of plugins that Telegraf supports.
See the [contributing guide](CONTRIBUTING.md) for instructions on writing
new plugins.

## Installation:

You can either download the binaries directly from the
[downloads](https://www.influxdata.com/downloads) page.

A few alternate installs are available here as well:

### FreeBSD tarball:

Latest:
* https://dl.influxdata.com/telegraf/releases/telegraf-VERSION_freebsd_amd64.tar.gz

### Ansible Role:

Ansible role: https://github.com/rossmcdonald/telegraf

### From Source:

Telegraf manages dependencies via [gdm](https://github.com/sparrc/gdm),
which gets installed via the Makefile
if you don't have it already. You also must build with golang version 1.8+.

1. [Install Go](https://golang.org/doc/install)
2. [Setup your GOPATH](https://golang.org/doc/code.html#GOPATH)
3. Run `go get github.com/influxdata/telegraf`
4. Run `cd $GOPATH/src/github.com/influxdata/telegraf`
5. Run `make`

## How to use it:

See usage with:

```
telegraf --help
```

#### Generate a telegraf config file:

```
telegraf config > telegraf.conf
```

#### Generate config with only cpu input & influxdb output plugins defined

```
telegraf --input-filter cpu --output-filter influxdb config
```

#### Run a single telegraf collection, outputing metrics to stdout

```
telegraf --config telegraf.conf -test
```

#### Run telegraf with all plugins defined in config file

```
telegraf --config telegraf.conf
```

#### Run telegraf, enabling the cpu & memory input, and influxdb output plugins

```
telegraf --config telegraf.conf -input-filter cpu:mem -output-filter influxdb
```


## Configuration

See the [configuration guide](docs/CONFIGURATION.md) for a rundown of the more advanced
configuration options.

## Input Plugins

* [amqp_consumer](./plugins/inputs/amqp_consumer) (rabbitmq)
* [apache](./plugins/inputs/apache)
* [cgroup](./plugins/inputs/cgroup)
* [conntrack](./plugins/inputs/conntrack)
* [dns query time](./plugins/inputs/dns_query)
* [elasticsearch](./plugins/inputs/elasticsearch)
* [exec](./plugins/inputs/exec) (generic executable plugin, support JSON, influx)
* [filestat](./plugins/inputs/filestat)
* [haproxy](./plugins/inputs/haproxy)
* [http_response](./plugins/inputs/http_response)
* [httpjson](./plugins/inputs/httpjson) (generic JSON-emitting http service plugin)
* [internal](./plugins/inputs/internal)
* [influxdb](./plugins/inputs/influxdb)
* [interrupts](./plugins/inputs/interrupts)
* [iptables](./plugins/inputs/iptables)
* [mongodb](./plugins/inputs/mongodb)
* [mysql](./plugins/inputs/mysql)
* [net_response](./plugins/inputs/net_response)
* [nginx](./plugins/inputs/nginx)
* [nstat](./plugins/inputs/nstat)
* [ntpq](./plugins/inputs/ntpq)
* [phpfpm](./plugins/inputs/phpfpm)
* [ping](./plugins/inputs/ping)
* [procstat](./plugins/inputs/procstat)
* [prometheus](./plugins/inputs/prometheus)
* [rabbitmq](./plugins/inputs/rabbitmq)
* [redis](./plugins/inputs/redis)
* [sensors](./plugins/inputs/sensors)
* [snmp](./plugins/inputs/snmp)
* [zookeeper](./plugins/inputs/zookeeper)
* [sysstat](./plugins/inputs/sysstat)
* [system](./plugins/inputs/system)
    * cpu
    * mem
    * net
    * netstat
    * disk
    * diskio
    * swap
    * processes
    * kernel (/proc/stat)
    * kernel (/proc/vmstat)
    * linux_sysctl_fs (/proc/sys/fs)

Telegraf can also collect metrics via the following service plugins:

* [http_listener](./plugins/inputs/http_listener)
* [nats_consumer](./plugins/inputs/nats_consumer)
* [logparser](./plugins/inputs/logparser)
* [statsd](./plugins/inputs/statsd)
* [socket_listener](./plugins/inputs/socket_listener)
* [tail](./plugins/inputs/tail)
* [tcp_listener](./plugins/inputs/socket_listener)
* [udp_listener](./plugins/inputs/socket_listener)
* [webhooks](./plugins/inputs/webhooks)
  * [github](./plugins/inputs/webhooks/github)


Telegraf is able to parse the following input data formats into metrics, these
formats may be used with input plugins supporting the `data_format` option:

* [InfluxDB Line Protocol](./docs/DATA_FORMATS_INPUT.md#influx)
* [JSON](./docs/DATA_FORMATS_INPUT.md#json)
* [Value](./docs/DATA_FORMATS_INPUT.md#value)


## Processor Plugins

* [printer](./plugins/processors/printer)

## Aggregator Plugins

* [minmax](./plugins/aggregators/minmax)

## Output Plugins

* [influxdb](./plugins/outputs/influxdb)
* [elasticsearch](./plugins/outputs/elasticsearch)
* [file](./plugins/outputs/file)
* [prometheus](./plugins/outputs/prometheus_client)
* [socket_writer](./plugins/outputs/socket_writer)
* [tcp](./plugins/outputs/socket_writer)
* [udp](./plugins/outputs/socket_writer)

## Contributing

Please see the
[contributing guide](CONTRIBUTING.md)
for details on contributing a plugin to Telegraf.
