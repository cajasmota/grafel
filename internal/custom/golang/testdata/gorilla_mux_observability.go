package fixtures

import (
	"context"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
)

var gorillaReqCount = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "gorilla_http_requests_total",
		Help: "Total HTTP requests handled by the gorilla/mux router.",
	},
	[]string{"route"},
)

func newGorillaServer() *mux.Router {
	r := mux.NewRouter()

	log := logrus.New()
	log.WithFields(logrus.Fields{"component": "router"}).Info("starting gorilla")

	r.HandleFunc("/users", func(w http.ResponseWriter, req *http.Request) {
		tracer := otel.Tracer("gorilla-service")
		ctx, span := tracer.Start(context.Background(), "handler.users.list")
		defer span.End()

		gorillaReqCount.WithLabelValues("/users").Inc()
		_ = ctx
		w.WriteHeader(http.StatusOK)
	}).Methods("GET")
	return r
}
