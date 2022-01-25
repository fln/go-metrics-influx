package influx_test

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
	if err := metrics.Register("foo", c); err != nil {
		// Handle err.
	}

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

	// Stop reporter goroutine after 5 minutes.
	time.Sleep(5 * time.Minute)
	stop()

	// Wait for graceful shutdown.
	wg.Wait()
}
