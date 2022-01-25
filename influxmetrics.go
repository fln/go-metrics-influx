// Package influx is a go-metrics to influx DB reporter implementation.
package influx

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	influxdb "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
	"github.com/influxdata/influxdb-client-go/v2/api/write"
	"github.com/influxdata/influxdb-client-go/v2/log"
	metrics "github.com/rcrowley/go-metrics"
	"github.com/sirupsen/logrus"
)

// Reporter holds configuration of go-metrics influx exporter.
// It should only be created using New function.
type Reporter struct {
	log                     logrus.FieldLogger
	registry                metrics.Registry
	url, token, org, bucket string
	interval                time.Duration
	retries                 uint
	tags                    map[string]string
	precision               time.Duration
	lastCounter             map[string]int64
}

// Option allows to configure optional reporter parameters.
type Option func(*Reporter)

// Logger sets custom logrus logger for error reporting.
func Logger(log logrus.FieldLogger) Option {
	return func(r *Reporter) {
		r.log = log
	}
}

// Tags sets a set of tags that will be assiciated with each influx data point
// written by this exporter.
func Tags(tags map[string]string) Option {
	return func(r *Reporter) {
		r.tags = tags
	}
}

// Interval specifies how often metrics should be reported to influxdb. By
// default it will be done every 10 seconds.
func Interval(intv time.Duration) Option {
	return func(r *Reporter) {
		r.interval = intv
	}
}

// Precision changes the duration precision used in reported data points. By
// default timestamps are reported with a 1 second precision. Having higher than
// seconds precision should be useful only when export interval is less
// than a second.
func Precision(prec time.Duration) Option {
	return func(r *Reporter) {
		r.precision = prec
	}
}

// Retries sets retries count after write failure. By default 3 retries are
// done.
func Retries(retries uint) Option {
	return func(r *Reporter) {
		r.retries = retries
	}
}

// New creates a new instance of influx metrics reporter. Variadic function
// parameters can be used to further configure reporter.
// It will not start exporting metrics until Run() is called.
func New(
	reg metrics.Registry,
	url string,
	token string,
	org string,
	bucket string,
	opts ...Option,
) *Reporter {

	r := &Reporter{
		log: &logrus.Logger{
			Out:       io.Discard,
			Formatter: new(logrus.TextFormatter),
			Hooks:     make(logrus.LevelHooks),
			Level:     logrus.PanicLevel,
		},
		url:         url,
		token:       token,
		org:         org,
		bucket:      bucket,
		retries:     3,
		registry:    reg,
		interval:    10 * time.Second,
		precision:   time.Second,
		lastCounter: make(map[string]int64),
	}

	for _, opt := range opts {
		opt(r)
	}

	// We disable influxdb client logger, as we're replacing it with
	// our own.
	log.Log = nil

	return r
}

// Run starts exporting metrics to influx DB. This method will block until
// context is cancelled. After context is closed, reporter client will be
// closed as well.
func (r *Reporter) Run(ctx context.Context) {
	client := influxdb.NewClientWithOptions(
		r.url,
		r.token,
		influxdb.DefaultOptions().SetHTTPClient(&http.Client{
			Timeout: r.interval,
		}).SetPrecision(r.precision).SetMaxRetries(r.retries),
	)

	rapi := client.WriteAPI(r.org, r.bucket)
	errCh := rapi.Errors()

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for err := range errCh {
			r.log.WithField("error", err).
				Error("writing metrics batch to influx database")
		}
	}()

	tc := time.NewTicker(r.interval)
	defer tc.Stop()

	for {
		select {
		case <-ctx.Done():
			client.Close()
			wg.Wait()
			return
		case tstamp := <-tc.C:
			r.report(rapi, tstamp)
		}
	}
}

// report send current snapshot of metrics registry to influx DB.
func (r *Reporter) report(rapi api.WriteAPI, tstamp time.Time) {
	r.registry.Each(func(name string, i interface{}) {
		var point *write.Point

		tags := make(map[string]string)
		for key, val := range r.tags {
			tags[key] = val
		}

		measurement := name
		if parts := strings.Split(name, ","); len(parts) > 1 {
			measurement = parts[0]
			for i := 1; i < len(parts); i++ {
				kv := strings.Split(parts[i], "=")
				if len(kv) == 2 {
					tags[kv[0]] = kv[1]
					continue
				}

				measurement = fmt.Sprintf("%s,%s", measurement, parts[i])
			}
		}

		switch metric := i.(type) {
		case metrics.Counter:
			count := metric.Count()
			diff := count - r.lastCounter[name]
			if diff < 0 {
				diff = count
			}

			r.lastCounter[name] = count
			point = write.NewPoint(
				measurement,
				tags,
				map[string]interface{}{
					"count": count,
					"diff":  diff,
				},
				tstamp,
			)
		case metrics.Gauge:
			point = write.NewPoint(
				measurement,
				tags,
				map[string]interface{}{
					"value": metric.Value(),
				},
				tstamp,
			)
		case metrics.GaugeFloat64:
			point = write.NewPoint(
				measurement,
				tags,
				map[string]interface{}{
					"value": metric.Value(),
				},
				tstamp,
			)
		case metrics.Histogram:
			ms := metric.Snapshot()
			ps := ms.Percentiles([]float64{0.5, 0.75, 0.95, 0.99, 0.999, 0.9999})
			point = write.NewPoint(
				measurement,
				tags,
				map[string]interface{}{
					"count":    ms.Count(),
					"max":      ms.Max(),
					"mean":     ms.Mean(),
					"min":      ms.Min(),
					"stddev":   ms.StdDev(),
					"variance": ms.Variance(),
					"p50":      ps[0],
					"p75":      ps[1],
					"p95":      ps[2],
					"p99":      ps[3],
					"p999":     ps[4],
					"p9999":    ps[5],
				},
				tstamp,
			)
		case metrics.Meter:
			ms := metric.Snapshot()
			point = write.NewPoint(
				measurement,
				tags,
				map[string]interface{}{
					"count": ms.Count(),
					"m1":    ms.Rate1(),
					"m5":    ms.Rate5(),
					"m15":   ms.Rate15(),
					"mean":  ms.RateMean(),
				},
				tstamp,
			)
		case metrics.Timer:
			ms := metric.Snapshot()
			ps := ms.Percentiles([]float64{0.5, 0.75, 0.95, 0.99, 0.999, 0.9999})
			point = write.NewPoint(
				measurement,
				tags,
				map[string]interface{}{
					"count":    ms.Count(),
					"max":      ms.Max(),
					"mean":     ms.Mean(),
					"min":      ms.Min(),
					"stddev":   ms.StdDev(),
					"variance": ms.Variance(),
					"p50":      ps[0],
					"p75":      ps[1],
					"p95":      ps[2],
					"p99":      ps[3],
					"p999":     ps[4],
					"p9999":    ps[5],
					"m1":       ms.Rate1(),
					"m5":       ms.Rate5(),
					"m15":      ms.Rate15(),
					"meanrate": ms.RateMean(),
				},
				tstamp,
			)
		default:
			// Unhandled metric type
			return
		}

		rapi.WritePoint(point)
	})

	rapi.Flush()
}
