// Fiber handler definitions.
package chifiber

import (
	"github.com/gofiber/fiber/v2"
)

// listWidgets handles GET /widgets.
func listWidgets(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"widgets": []string{"x", "y"}})
}

// createWidget handles POST /widgets.
func createWidget(c *fiber.Ctx) error {
	return c.Status(201).JSON(fiber.Map{"id": 7})
}
