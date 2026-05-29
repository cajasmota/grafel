package fixtures

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
)

var hertzReqCount = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "hertz_http_requests_total",
		Help: "Total HTTP requests handled by the hertz engine.",
	},
	[]string{"route"},
)

func newHertzServer() {
	h := server.Default()

	log := logrus.New()
	log.WithFields(logrus.Fields{"component": "router"}).Info("starting hertz")

	h.GET("/users", func(ctx context.Context, c *app.RequestContext) {
		tracer := otel.Tracer("hertz-service")
		spanCtx, span := tracer.Start(ctx, "handler.users.list")
		defer span.End()

		hertzReqCount.WithLabelValues("/users").Inc()
		_ = spanCtx
		c.JSON(200, map[string]any{"ok": true})
	})
	h.Spin()
}
