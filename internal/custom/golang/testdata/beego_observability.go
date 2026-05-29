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
		Help: "Total HTTP requests.",
	},
	[]string{"path"},
)

// UserController is a beego controller; the package uses web.Router so the
// observability scanner attributes this file to beego.
type BeegoUserController struct {
	web.Controller
}

func (c *BeegoUserController) Get() {
	log := logrus.New()
	log.WithFields(logrus.Fields{"component": "beego"}).Info("get users")

	tracer := otel.Tracer("beego-service")
	_, span := tracer.Start(context.Background(), "handler.beego.users.list")
	defer span.End()

	beegoReqCount.WithLabelValues("/users").Inc()
	c.Ctx.WriteString("ok")
}

func registerBeego() {
	web.Router("/users", &BeegoUserController{}, "get:Get")
	web.Run()
}
