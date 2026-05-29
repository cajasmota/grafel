package fixtures

import (
	"github.com/kataras/iris/v12"
)

// IrisLoginReq is an iris-bound DTO using validate: struct tags.
type IrisLoginReq struct {
	Username string `json:"username" validate:"required,alphanum"`
	Password string `json:"password" validate:"required,min=8"`
}

func setupIrisVal() {
	app := iris.New()
	app.Post("/login", func(ctx iris.Context) {
		var req IrisLoginReq
		if err := ctx.ReadJSON(&req); err != nil {
			ctx.StopWithError(400, err)
			return
		}
		ctx.JSON(req)
	})
}
