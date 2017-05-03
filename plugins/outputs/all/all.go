package all

import (
	_ "github.com/influxdata/telegraf/plugins/outputs/elasticsearch"
	_ "github.com/influxdata/telegraf/plugins/outputs/file"
	_ "github.com/influxdata/telegraf/plugins/outputs/influxdb"
	_ "github.com/influxdata/telegraf/plugins/outputs/prometheus_client"
	_ "github.com/influxdata/telegraf/plugins/outputs/socket_writer"
)
