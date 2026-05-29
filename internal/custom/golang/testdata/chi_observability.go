package fixtures

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
)

var chiSummary = prometheus.NewSummaryVec(
	prometheus.SummaryOpts{
		Name: "chi_response_size_bytes",
		Help: "Response sizes.",
	},
	[]string{"route"},
)

func newChiRouter() http.Handler {
	log := logrus.WithFields(logrus.Fields{"component": "router"})
	log.Info("starting")

	r := chi.NewRouter()
	r.Get("/health", func(w http.ResponseWriter, req *http.Request) {
		tracer := otel.Tracer("chi-service")
		ctx, span := tracer.Start(req.Context(), "handler.health")
		defer span.End()
		_ = ctx
		w.WriteHeader(http.StatusOK)
	})
	return r
}
