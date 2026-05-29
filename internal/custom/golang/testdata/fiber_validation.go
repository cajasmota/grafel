package fixtures

import (
	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
)

// SignupReq is a fiber-bound DTO using validate: struct tags.
type SignupReq struct {
	Email string `json:"email" validate:"required,email"`
	Phone string `json:"phone" validate:"required,e164"`
}

func setupFiber() {
	app := fiber.New()
	validate := validator.New()

	app.Post("/signup", func(c *fiber.Ctx) error {
		var req SignupReq
		if err := c.BodyParser(&req); err != nil {
			return err
		}
		if err := validate.Struct(&req); err != nil {
			return err
		}
		return c.JSON(req)
	})
}
