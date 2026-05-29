package fixtures

import (
	"context"

	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	"go.uber.org/zap"
)

var echoLatency = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Name: "echo_request_duration_seconds",
		Help: "Request latency.",
	},
	[]string{"route"},
)

func newEchoServer() *echo.Echo {
	logger, _ := zap.NewProduction()
	sugar := logger.With(zap.String("component", "router"))
	_ = sugar

	e := echo.New()
	e.GET("/orders", func(c echo.Context) error {
		tracer := otel.Tracer("echo-service")
		ctx, span := tracer.Start(context.Background(), "handler.orders.list")
		defer span.End()
		_ = ctx
		return c.JSON(200, map[string]bool{"ok": true})
	})
	return e
}
