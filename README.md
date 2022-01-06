go-metrics-influx
=================

[![Godoc](http://img.shields.io/badge/godoc-reference-blue.svg?style=flat)](https://godoc.org/github.com/dbadv/go-metrics-influx)

This is a reporter for the [go-metrics](https://github.com/rcrowley/go-metrics)
library which will post the metrics to [InfluxDB](https://influxdb.com/).

It is loosely based on the
[vrischmann/go-metrics-influxdb](https://github.com/vrischmann/go-metrics-influxdb)
implementation but with the following changes:

- Support for newer Influx DB version v2.0+.
- Optional settings can be set by using variadic function parameters.
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
	"sync"
	"time"

	influx "github.com/dbadv/go-metrics-influx"
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

func Example() {
	ctx, stop := context.WithCancel(context.Background())

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		influx.New(
			metrics.DefaultRegistry, // go-metrics registry
			"http://localhost:8086", // influx URL
			"user:pass",             // influx token
			"org",                   // org name
			"bucket",                // bucket name
			influx.Tags(map[string]string{"instance": "app@localhost"}),
			influx.Logger(logrus.WithField("thread", "go-metrics-influx")),
		).Run(ctx)
	}()

	// ...
	go worker()
	// ...

	// Stop reporter goroutine after 5 minutes
	time.Sleep(5 * time.Minute)
	stop()

	// Wait for graceful shutdown.
	wg.Wait()
}
```
