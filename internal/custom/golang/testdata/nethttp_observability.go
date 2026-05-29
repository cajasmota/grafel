package fixtures

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
)

var netReqCount = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "nethttp_http_requests_total",
		Help: "Total HTTP requests.",
	},
	[]string{"path"},
)

func newNetHTTPMux() *http.ServeMux {
	log := logrus.New()
	log.WithFields(logrus.Fields{"component": "net-http"}).Info("starting")

	mux := http.NewServeMux()
	mux.HandleFunc("/users", func(w http.ResponseWriter, req *http.Request) {
		tracer := otel.Tracer("nethttp-service")
		_, span := tracer.Start(req.Context(), "handler.nethttp.users.list")
		defer span.End()
		netReqCount.WithLabelValues("/users").Inc()
		w.WriteHeader(200)
	})
	return mux
}
