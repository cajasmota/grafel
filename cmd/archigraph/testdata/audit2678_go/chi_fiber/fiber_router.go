// Fiber router file.
package chifiber

import (
	"github.com/gofiber/fiber/v2"
)

func setupFiber() *fiber.App {
	app := fiber.New()
	app.Get("/widgets", listWidgets)
	app.Post("/widgets", createWidget)
	return app
}
