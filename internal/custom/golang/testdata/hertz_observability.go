package fixtures

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	"go.uber.org/zap"
)

var hertzReqCount = prometheus.NewCounter(
	prometheus.CounterOpts{
		Name: "hertz_http_requests_total",
		Help: "Total HTTP requests.",
	},
)

func newHertzServer() *server.Hertz {
	logger, _ := zap.NewProduction()
	logger.With(zap.String("component", "hertz")).Info("starting")

	h := server.Default()
	h.GET("/users", func(c context.Context, ctx *app.RequestContext) {
		tracer := otel.Tracer("hertz-service")
		_, span := tracer.Start(c, "handler.hertz.users.list")
		defer span.End()
		hertzReqCount.Inc()
		ctx.JSON(200, map[string]any{"ok": true})
	})
	return h
}
