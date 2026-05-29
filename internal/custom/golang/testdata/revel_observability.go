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
		Help: "Total HTTP requests handled by the revel app.",
	},
	[]string{"action"},
)

// RevelApp is a revel controller embedding *revel.Controller.
type RevelApp struct {
	*revel.Controller
}

func (c RevelApp) Index() revel.Result {
	log := logrus.New()
	log.WithField("component", "users").Info("index action")

	tracer := otel.Tracer("revel-service")
	ctx, span := tracer.Start(context.Background(), "action.users.index")
	defer span.End()

	revelReqCount.WithLabelValues("App.Index").Inc()
	_ = ctx
	return c.RenderJSON(map[string]any{"ok": true})
}

func init() {
	revel.InterceptMethod(RevelApp.Index, revel.BEFORE)
}
