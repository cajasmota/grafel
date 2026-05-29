package fixtures

import (
	"context"

	"github.com/kataras/iris/v12"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	"go.uber.org/zap"
)

var irisReqLatency = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Name: "iris_request_duration_seconds",
		Help: "Request latency.",
	},
	[]string{"route"},
)

func newIrisServer() *iris.Application {
	logger, _ := zap.NewProduction()
	logger.With(zap.String("component", "iris")).Info("starting")

	app := iris.New()
	app.Get("/users", func(ctx iris.Context) {
		tracer := otel.Tracer("iris-service")
		_, span := tracer.Start(context.Background(), "handler.iris.users.list")
		defer span.End()
		irisReqLatency.WithLabelValues("/users").Observe(0.1)
		ctx.JSON(iris.Map{"ok": true})
	})
	return app
}
