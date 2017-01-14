package influx_test

import (
	"context"
	"time"

	"github.com/Sirupsen/logrus"
	influx "github.com/fln/go-metrics-influx"
	metrics "github.com/rcrowley/go-metrics"
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
	// Create context with cancel callback
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
