package fixtures

import (
	"context"
	"log/slog"
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
)

var fiberErrors = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "fiber_inflight_requests",
		Help: "In-flight requests.",
	},
	[]string{"route"},
)

func newFiberServer() *fiber.App {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	logger = logger.With("component", "router")
	_ = logger

	app := fiber.New()
	app.Get("/items", func(c *fiber.Ctx) error {
		tracer := otel.Tracer("fiber-service")
		ctx, span := tracer.Start(context.Background(), "handler.items.list")
		defer span.End()
		_ = ctx
		return c.JSON(map[string]bool{"ok": true})
	})
	return app
}
