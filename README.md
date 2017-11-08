go-metrics-influx
=================

[![Godoc](http://img.shields.io/badge/godoc-reference-blue.svg?style=flat)](https://godoc.org/github.com/fln/go-metrics-influx)

This is a reporter for the [go-metrics](https://github.com/rcrowley/go-metrics)
library which will post the metrics to [InfluxDB](https://influxdb.com/).

It is loosely based on the
[vrischmann/go-metrics-influxdb](https://github.com/vrischmann/go-metrics-influxdb)
implementation but with the following changes:

- Support for newer Influx DB version V1.1+.
- Optional settings can be set by chaining setter methods.
- Support for structured logging of errors via [logrus](vrischmann/go-metrics-influxdb).
- Support for stopping reporter via context.
- Tags can be passed as `key=val` pairs separated by `,` in the metrics name
  (similar to influx line protocol).

Tag pairs extraction from metrics name example:

- Metric with name `hit_counter,region=eu,customerID=15` would create
  `hit_counter` measurement with tags `{"region": "eu", "customerID": "15"}`.
- Invalid tag pairs are kept in the measurement name. Metric with a name
  `hit_counter,smth,a=b` would create `hit_counter,smth` measurement with tags
  `{"a": "b"}`.

Usage
-----

```go
package main

import (
	"context"
	"time"

        influx "github.com/fln/go-metrics-influx"
        metrics "github.com/rcrowley/go-metrics"
        "github.com/sirupsen/logrus"
)


func worker() {
	c := metrics.NewCounter()
	metrics.Register("foo", c)

	for {
		c.Inc(1)
		time.Sleep(time.Second)
	}
}

func main() {
	// Create context with cancel
	ctx, stop := context.WithCancel(context.Background())

	go influx.NewReporter(
		metrics.DefaultRegistry,           // go-metrics registry
		5*time.Second,                     // report interval
		"http://user:pass@localhost:8086", // Influx URL and credentials
		"app-metrics",                     // databse name
	).
		Tags(map[string]string{"instance": "app@localhost"}).
		Context(ctx).
		Logger(logrus.WithField("thread", "go-metrics-influx")).
		Run()

	// ...
	go worker()
	// ...

	// Stop reporter goroutine after 5 minutes
	time.Sleep(5 * time.Minute)
	stop()
}
```
