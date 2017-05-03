# Telegraf Input Data Formats

Telegraf is able to parse the following input data formats into metrics:

1. [InfluxDB Line Protocol](https://github.com/influxdata/telegraf/blob/master/docs/DATA_FORMATS_INPUT.md#influx)
1. [JSON](https://github.com/influxdata/telegraf/blob/master/docs/DATA_FORMATS_INPUT.md#json)
1. [Value](https://github.com/influxdata/telegraf/blob/master/docs/DATA_FORMATS_INPUT.md#value), ie: 45 or "booyah"

Telegraf metrics, like InfluxDB
[points](https://docs.influxdata.com/influxdb/v0.10/write_protocols/line/),
are a combination of four basic parts:

1. Measurement Name
1. Tags
1. Fields
1. Timestamp

These four parts are easily defined when using InfluxDB line-protocol as a
data format. But there are other data formats that users may want to use which
require more advanced configuration to create usable Telegraf metrics.

Plugins such as `exec` and `kafka_consumer` parse textual data. Up until now,
these plugins were statically configured to parse just a single
data format. `exec` mostly only supported parsing JSON, and `kafka_consumer` only
supported data in InfluxDB line-protocol.

But now we are normalizing the parsing of various data formats across all
plugins that can support it. You will be able to identify a plugin that supports
different data formats by the presence of a `data_format` config option, for
example, in the exec plugin:

```toml
[[inputs.exec]]
  ## Commands array
  commands = ["/tmp/test.sh", "/usr/bin/mycollector --foo=bar"]

  ## measurement name suffix (for separating different commands)
  name_suffix = "_mycollector"

  ## Data format to consume.
  ## Each data format has its own unique set of configuration options, read
  ## more about them here:
  ## https://github.com/influxdata/telegraf/blob/master/docs/DATA_FORMATS_INPUT.md
  data_format = "json"

  ## Additional configuration options go here
```

Each data_format has an additional set of configuration options available, which
I'll go over below.

# Influx:

There are no additional configuration options for InfluxDB line-protocol. The
metrics are parsed directly into Telegraf metrics.

#### Influx Configuration:

```toml
[[inputs.exec]]
  ## Commands array
  commands = ["/tmp/test.sh", "/usr/bin/mycollector --foo=bar"]

  ## measurement name suffix (for separating different commands)
  name_suffix = "_mycollector"

  ## Data format to consume.
  ## Each data format has its own unique set of configuration options, read
  ## more about them here:
  ## https://github.com/influxdata/telegraf/blob/master/docs/DATA_FORMATS_INPUT.md
  data_format = "influx"
```

# JSON:

The JSON data format flattens JSON into metric _fields_.
NOTE: Only numerical values are converted to fields, and they are converted
into a float. strings are ignored unless specified as a tag_key (see below).

So for example, this JSON:

```json
{
    "a": 5,
    "b": {
        "c": 6
    },
    "ignored": "I'm a string"
}
```

Would get translated into _fields_ of a measurement:

```
myjsonmetric a=5,b_c=6
```

The _measurement_ _name_ is usually the name of the plugin,
but can be overridden using the `name_override` config option.

#### JSON Configuration:

The JSON data format supports specifying "tag keys". If specified, keys
will be searched for in the root-level of the JSON blob. If the key(s) exist,
they will be applied as tags to the Telegraf metrics.

For example, if you had this configuration:

```toml
[[inputs.exec]]
  ## Commands array
  commands = ["/tmp/test.sh", "/usr/bin/mycollector --foo=bar"]

  ## measurement name suffix (for separating different commands)
  name_suffix = "_mycollector"

  ## Data format to consume.
  ## Each data format has its own unique set of configuration options, read
  ## more about them here:
  ## https://github.com/influxdata/telegraf/blob/master/docs/DATA_FORMATS_INPUT.md
  data_format = "json"

  ## List of tag names to extract from top-level of JSON server response
  tag_keys = [
    "my_tag_1",
    "my_tag_2"
  ]
```

with this JSON output from a command:

```json
{
    "a": 5,
    "b": {
        "c": 6
    },
    "my_tag_1": "foo"
}
```

Your Telegraf metrics would get tagged with "my_tag_1"

```
exec_mycollector,my_tag_1=foo a=5,b_c=6
```

If the JSON data is an array, then each element of the array is parsed with the configured settings.
Each resulting metric will be output with the same timestamp.

For example, if the following configuration:

```toml
[[inputs.exec]]
  ## Commands array
  commands = ["/usr/bin/mycollector --foo=bar"]

  ## measurement name suffix (for separating different commands)
  name_suffix = "_mycollector"

  ## Data format to consume.
  ## Each data format has its own unique set of configuration options, read
  ## more about them here:
  ## https://github.com/influxdata/telegraf/blob/master/docs/DATA_FORMATS_INPUT.md
  data_format = "json"

  ## List of tag names to extract from top-level of JSON server response
  tag_keys = [
    "my_tag_1",
    "my_tag_2"
  ]
```

with this JSON output from a command:

```json
[
    {
        "a": 5,
        "b": {
            "c": 6
        },
        "my_tag_1": "foo",
        "my_tag_2": "baz"
    },
    {
        "a": 7,
        "b": {
            "c": 8
        },
        "my_tag_1": "bar",
        "my_tag_2": "baz"
    }
]
```

Your Telegraf metrics would get tagged with "my_tag_1" and "my_tag_2"

```
exec_mycollector,my_tag_1=foo,my_tag_2=baz a=5,b_c=6
exec_mycollector,my_tag_1=bar,my_tag_2=baz a=7,b_c=8
```

# Value:

The "value" data format translates single values into Telegraf metrics. This
is done by assigning a measurement name and setting a single field ("value")
as the parsed metric.

#### Value Configuration:

You **must** tell Telegraf what type of metric to collect by using the
`data_type` configuration option. Available options are:

1. integer
2. float or long
3. string
4. boolean

**Note:** It is also recommended that you set `name_override` to a measurement
name that makes sense for your metric, otherwise it will just be set to the
name of the plugin.

```toml
[[inputs.exec]]
  ## Commands array
  commands = ["cat /proc/sys/kernel/random/entropy_avail"]

  ## override the default metric name of "exec"
  name_override = "entropy_available"

  ## Data format to consume.
  ## Each data format has its own unique set of configuration options, read
  ## more about them here:
  ## https://github.com/influxdata/telegraf/blob/master/docs/DATA_FORMATS_INPUT.md
  data_format = "value"
  data_type = "integer" # required
```
