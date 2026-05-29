package fixtures

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
)

var netHTTPReqCount = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "nethttp_http_requests_total",
		Help: "Total HTTP requests handled by the net/http ServeMux.",
	},
	[]string{"route"},
)

func newNetHTTPServer() *http.ServeMux {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	logger.Info("starting net/http server")

	mux := http.NewServeMux()
	mux.HandleFunc("GET /users", func(w http.ResponseWriter, r *http.Request) {
		tracer := otel.Tracer("nethttp-service")
		ctx, span := tracer.Start(context.Background(), "handler.users.list")
		defer span.End()

		netHTTPReqCount.WithLabelValues("/users").Inc()
		_ = ctx
		w.WriteHeader(http.StatusOK)
	})
	return mux
}
