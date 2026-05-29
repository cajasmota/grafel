package fixtures

import (
	"context"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	"go.uber.org/zap"
)

var muxReqCount = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "mux_http_requests_total",
		Help: "Total HTTP requests.",
	},
	[]string{"path"},
)

func newMuxRouter() *mux.Router {
	logger, _ := zap.NewProduction()
	logger.With(zap.String("component", "gorilla-mux")).Info("starting")

	r := mux.NewRouter()
	r.HandleFunc("/users", func(w http.ResponseWriter, req *http.Request) {
		tracer := otel.Tracer("mux-service")
		_, span := tracer.Start(req.Context(), "handler.mux.users.list")
		defer span.End()
		muxReqCount.WithLabelValues("/users").Inc()
		w.WriteHeader(200)
	})
	return r
}

func _muxCtx() context.Context { return context.Background() }
