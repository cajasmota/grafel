package fixtures

import (
	"context"

	"github.com/gobuffalo/buffalo"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
)

var buffaloReqCount = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "buffalo_inflight_requests",
		Help: "In-flight requests.",
	},
	[]string{"path"},
)

func newBuffaloApp() *buffalo.App {
	log := logrus.New()
	log.WithFields(logrus.Fields{"component": "buffalo"}).Info("starting")

	app := buffalo.New(buffalo.Options{})
	app.GET("/users", func(c buffalo.Context) error {
		tracer := otel.Tracer("buffalo-service")
		_, span := tracer.Start(context.Background(), "handler.buffalo.users.list")
		defer span.End()
		buffaloReqCount.WithLabelValues("/users").Inc()
		return c.Render(200, nil)
	})
	return app
}
