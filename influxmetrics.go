// Package influx is a go-metrics to influx DB reporter implementation.
package influx

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/url"
	"strings"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
	"github.com/influxdata/influxdb-client-go/v2/api/write"
	metrics "github.com/rcrowley/go-metrics"
	"github.com/sirupsen/logrus"
)

// Reporter holds configuration of go-metrics influx exporter. It can be
// configured only be public setter methods.
type Reporter struct {
	registry  metrics.Registry
	interval  time.Duration
	url       string
	database  string
	tags      map[string]string
	precision time.Duration
	org       string

	ctx context.Context
	log logrus.FieldLogger

	lastCounter map[string]int64
}

// NewReporter creates a new instance of influx metrcs reporter. It may be
// further configured with helper methods. It will not start exporting metrics
// until Run() is called.
func NewReporter(registry metrics.Registry, interval time.Duration, url string, db string) *Reporter {
	return &Reporter{
		registry:  registry,
		interval:  interval,
		url:       url,
		database:  db,
		tags:      nil,
		precision: time.Second,
		org:       "",
		ctx:       context.Background(),
		log: &logrus.Logger{
			Out:       ioutil.Discard,
			Formatter: new(logrus.TextFormatter),
			Hooks:     make(logrus.LevelHooks),
			Level:     logrus.PanicLevel,
		},
		lastCounter: make(map[string]int64),
	}
}

// Tags sets a set of tags that will be assiciated with each influx data point
// written by this exporter.
func (r *Reporter) Tags(tags map[string]string) *Reporter {
	r.tags = tags
	return r
}

// Precision changes the timestamp precision used in reported data points. By
// default timestamps are reported with a seconds precision. Having higher than
// seconds precision should be useful only when export interval is less
// than a second.
func (r *Reporter) Precision(precision time.Duration) *Reporter {
	r.precision = precision
	return r
}

// Context assigns a context to this reporter. Context is only used to stop
// reporter Run() method.
func (r *Reporter) Context(ctx context.Context) *Reporter {
	r.ctx = ctx
	return r
}

// Logger sets optional logrus logger for error reporting.
func (r *Reporter) Logger(log logrus.FieldLogger) *Reporter {
	r.log = log
	return r
}

// Organization sets organization name. Used by influx cloud.
func (r *Reporter) Organization(org string) *Reporter {
	r.org = org
	return r
}

// Run starts exporting metrics to influx DB. This method will block until
// context associated with this reporter is stopper (of forever if contex is
// not set).
func (r *Reporter) Run() {
	url, err := url.Parse(r.url)
	if err != nil {
		r.log.WithField("url", r.url).WithError(err).Error("malformed influx server URL")
		return
	}

	token := url.User.String()
	url.User = nil

	c := influxdb2.NewClientWithOptions(url.String(), token,
		influxdb2.DefaultOptions().
			SetHTTPRequestTimeout(uint(r.interval/time.Second)).
			SetPrecision(r.precision).
			SetUseGZip(true),
	).WriteAPIBlocking(r.org, r.database)
	if err != nil {
		r.log.WithField("url", r.url).WithError(err).Error("creating new influx client")
		return
	}

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			r.report(r.ctx, c)
		case <-r.ctx.Done():
			return
		}
	}
}

// report send current snapshot of metrics registry to influx DB.
func (r *Reporter) report(ctx context.Context, c api.WriteAPIBlocking) {
	var points []*write.Point

	now := time.Now()
	r.registry.Each(func(name string, i interface{}) {
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
				} else {
					measurement = fmt.Sprintf("%s,%s", measurement, parts[i])
				}
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
			points = append(points, influxdb2.NewPoint(
				measurement,
				tags,
				map[string]interface{}{
					"count": count,
					"diff":  diff,
				},
				now,
			))
		case metrics.Gauge:
			points = append(points, influxdb2.NewPoint(
				measurement,
				tags,
				map[string]interface{}{
					"value": metric.Value(),
				},
				now,
			))
		case metrics.GaugeFloat64:
			points = append(points, influxdb2.NewPoint(

				measurement,
				tags,
				map[string]interface{}{
					"value": metric.Value(),
				},
				now,
			))
		case metrics.Histogram:
			ms := metric.Snapshot()
			ps := ms.Percentiles([]float64{0.5, 0.75, 0.95, 0.99, 0.999, 0.9999})
			points = append(points, influxdb2.NewPoint(
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
				now,
			))
		case metrics.Meter:
			ms := metric.Snapshot()
			points = append(points, influxdb2.NewPoint(
				measurement,
				tags,
				map[string]interface{}{
					"count": ms.Count(),
					"m1":    ms.Rate1(),
					"m5":    ms.Rate5(),
					"m15":   ms.Rate15(),
					"mean":  ms.RateMean(),
				},
				now,
			))
		case metrics.Timer:
			ms := metric.Snapshot()
			ps := ms.Percentiles([]float64{0.5, 0.75, 0.95, 0.99, 0.999, 0.9999})
			points = append(points, influxdb2.NewPoint(
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
				now,
			))
		default:
			// Unhandled metric type
			return
		}
	})

	if len(points) == 0 {
		return
	}
	if err := c.WritePoint(ctx, points...); err != nil {
		r.log.WithError(err).Error("writing data points to influx")
	}
}
