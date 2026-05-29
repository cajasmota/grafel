package fixtures

import (
	"context"

	"github.com/gobuffalo/buffalo"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	"go.uber.org/zap"
)

var buffaloReqCount = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "buffalo_http_requests_total",
		Help: "Total HTTP requests handled by the buffalo app.",
	},
	[]string{"route"},
)

func usersList(c buffalo.Context) error {
	logger, _ := zap.NewProduction()
	logger = logger.With(zap.String("component", "users"))
	logger.Info("listing users")

	tracer := otel.Tracer("buffalo-service")
	ctx, span := tracer.Start(context.Background(), "handler.users.list")
	defer span.End()

	buffaloReqCount.WithLabelValues("/users").Inc()
	_ = ctx
	return c.Render(200, nil)
}

func newBuffaloServer() *buffalo.App {
	app := buffalo.New(buffalo.Options{})
	app.GET("/users", usersList)
	return app
}
