package fixtures

import (
	"context"

	"github.com/kataras/iris/v12"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	"go.uber.org/zap"
)

var irisReqCount = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "iris_http_requests_total",
		Help: "Total HTTP requests handled by the iris app.",
	},
	[]string{"route"},
)

func newIrisServer() *iris.Application {
	app := iris.New()

	logger, _ := zap.NewProduction()
	logger = logger.With(zap.String("component", "router"))
	logger.Info("starting iris")

	app.Get("/users", func(c iris.Context) {
		tracer := otel.Tracer("iris-service")
		ctx, span := tracer.Start(context.Background(), "handler.users.list")
		defer span.End()

		irisReqCount.WithLabelValues("/users").Inc()
		_ = ctx
		c.JSON(iris.Map{"ok": true})
	})
	return app
}
