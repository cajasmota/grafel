package fixtures

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/revel/revel"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
)

var revelReqCount = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "revel_http_requests_total",
		Help: "Total HTTP requests.",
	},
	[]string{"action"},
)

// RevelUserController is a revel controller; the package references
// revel.Controller / revel.Result so the scanner attributes this to revel.
type RevelUserController struct {
	*revel.Controller
}

func (c RevelUserController) List() revel.Result {
	log := logrus.New()
	log.WithFields(logrus.Fields{"component": "revel"}).Info("list users")

	tracer := otel.Tracer("revel-service")
	_, span := tracer.Start(context.Background(), "handler.revel.users.list")
	defer span.End()

	revelReqCount.WithLabelValues("List").Inc()
	return c.RenderJSON(map[string]any{"ok": true})
}
