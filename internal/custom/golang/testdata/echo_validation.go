package fixtures

import (
	"github.com/go-playground/validator/v10"
	"github.com/labstack/echo/v4"
)

// LoginReq is an echo-bound DTO using validate: struct tags (go-playground).
type LoginReq struct {
	Username string `json:"username" validate:"required,alphanum"`
	Password string `json:"password" validate:"required,min=8"`
}

type echoValidator struct{ v *validator.Validate }

func (ev *echoValidator) Validate(i interface{}) error { return ev.v.Struct(i) }

func setupEcho() {
	e := echo.New()
	e.Validator = &echoValidator{v: validator.New()}

	e.POST("/login", func(c echo.Context) error {
		var req LoginReq
		if err := c.Bind(&req); err != nil {
			return err
		}
		if err := c.Validate(&req); err != nil {
			return err
		}
		return c.JSON(200, req)
	})
}
