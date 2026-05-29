package testdata

import "github.com/gofiber/fiber/v2"

// SignupReq is the request DTO parsed by fiber's BodyParser.
type SignupReq struct {
	Email string `json:"email" validate:"required,email"`
	Name  string `json:"name"`
}

// SignupResp is the response DTO serialised by c.JSON.
type SignupResp struct {
	ID      int    `json:"id"`
	Message string `json:"message"`
}

func signup(c *fiber.Ctx) error {
	var req SignupReq
	if err := c.BodyParser(&req); err != nil {
		return err
	}
	resp := SignupResp{ID: 7, Message: "ok"}
	return c.JSON(resp)
}

func setupFiber() {
	app := fiber.New()
	app.Post("/signup", signup)
	_ = app
}
