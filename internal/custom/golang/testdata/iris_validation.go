package fixtures

import (
	"github.com/go-playground/validator/v10"
	"github.com/kataras/iris/v12"
)

// IrisLoginReq is a DTO validated through iris's built-in go-playground
// validator (app.Validator = validator.New()) using validate: tags.
type IrisLoginReq struct {
	Username string `json:"username" validate:"required,alphanum"`
	Password string `json:"password" validate:"required,min=8"`
}

func newIrisValidationApp() *iris.Application {
	app := iris.New()
	app.Validator = validator.New()

	app.Post("/login", func(c iris.Context) {
		var req IrisLoginReq
		if err := c.ReadJSON(&req); err != nil {
			c.StopWithStatus(400)
			return
		}
		c.JSON(req)
	})
	return app
}
