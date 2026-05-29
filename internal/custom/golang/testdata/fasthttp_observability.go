package fixtures

import (
	"context"
	"log/slog"
	"os"

	"github.com/fasthttp/router"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/valyala/fasthttp"
	"go.opentelemetry.io/otel"
)

var fastReqCount = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "fasthttp_http_requests_total",
		Help: "Total HTTP requests handled by the fasthttp router.",
	},
	[]string{"route"},
)

func fastUsersList(ctx *fasthttp.RequestCtx) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	logger.Info("listing users")

	tracer := otel.Tracer("fasthttp-service")
	c, span := tracer.Start(context.Background(), "handler.users.list")
	defer span.End()

	fastReqCount.WithLabelValues("/users").Inc()
	_ = c
	ctx.SetStatusCode(fasthttp.StatusOK)
}

func newFastServer() *router.Router {
	r := router.New()
	r.GET("/users", fastUsersList)
	return r
}
