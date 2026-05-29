package fixtures

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
)

var reqCount = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "gin_http_requests_total",
		Help: "Total HTTP requests.",
	},
	[]string{"path"},
)

func newGinServer() *gin.Engine {
	log := logrus.New()
	log.WithFields(logrus.Fields{"component": "router"}).Info("starting")

	r := gin.Default()
	r.GET("/users", func(c *gin.Context) {
		tracer := otel.Tracer("gin-service")
		ctx, span := tracer.Start(context.Background(), "handler.users.list")
		defer span.End()
		reqCount.WithLabelValues("/users").Inc()
		_ = ctx
		c.JSON(200, gin.H{"ok": true})
	})
	return r
}
