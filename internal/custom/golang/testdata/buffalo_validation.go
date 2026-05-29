package fixtures

import (
	"github.com/gobuffalo/buffalo"
)

// BuffaloSignupReq is a DTO bound via c.Bind inside a buffalo handler and
// validated through go-playground validate: tags.
type BuffaloSignupReq struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=8"`
}

func buffaloSignup(c buffalo.Context) error {
	var req BuffaloSignupReq
	if err := c.Bind(&req); err != nil {
		return c.Render(400, nil)
	}
	return c.Render(201, nil)
}

func newBuffaloValidationApp() *buffalo.App {
	app := buffalo.New(buffalo.Options{})
	app.POST("/signup", buffaloSignup)
	return app
}
