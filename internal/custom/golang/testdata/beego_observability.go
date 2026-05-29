package fixtures

import (
	"context"

	"github.com/beego/beego/v2/server/web"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
)

var beegoReqCount = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "beego_http_requests_total",
		Help: "Total HTTP requests handled by the beego app.",
	},
	[]string{"controller"},
)

// BeegoUserController is a beego controller embedding web.Controller.
type BeegoUserController struct {
	web.Controller
}

func (c *BeegoUserController) GetAll() {
	log := logrus.New()
	log.WithFields(logrus.Fields{"component": "users"}).Info("listing users")

	tracer := otel.Tracer("beego-service")
	ctx, span := tracer.Start(context.Background(), "controller.users.list")
	defer span.End()

	beegoReqCount.WithLabelValues("UserController").Inc()
	_ = ctx
	c.Data["json"] = map[string]any{"ok": true}
	c.ServeJSON()
}

func newBeegoServer() {
	web.Router("/users", &BeegoUserController{}, "get:GetAll")
	web.Run()
}
