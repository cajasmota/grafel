package fixtures

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/valyala/fasthttp"
	"go.opentelemetry.io/otel"
	"go.uber.org/zap"
)

var fastReqCount = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "fasthttp_http_requests_total",
		Help: "Total HTTP requests.",
	},
	[]string{"path"},
)

func fastHandler(ctx *fasthttp.RequestCtx) {
	logger, _ := zap.NewProduction()
	logger.With(zap.String("component", "fasthttp")).Info("handling")

	tracer := otel.Tracer("fasthttp-service")
	_, span := tracer.Start(context.Background(), "handler.fasthttp.users.list")
	defer span.End()

	fastReqCount.WithLabelValues(string(ctx.Path())).Inc()
	ctx.SetStatusCode(200)
}

func runFast() {
	_ = fasthttp.ListenAndServe(":8080", fastHandler)
}
