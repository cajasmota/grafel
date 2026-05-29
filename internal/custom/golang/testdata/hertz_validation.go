package fixtures

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
)

// HertzSignupReq is a hertz-bound DTO using vd/validate: struct tags.
type HertzSignupReq struct {
	Email string `json:"email" validate:"required,email"`
	Age   int    `json:"age" validate:"gte=0,lte=130"`
}

func setupHertzVal() {
	h := server.Default()
	h.POST("/signup", func(c context.Context, ctx *app.RequestContext) {
		var req HertzSignupReq
		if err := ctx.BindAndValidate(&req); err != nil {
			ctx.JSON(400, map[string]any{"error": err.Error()})
			return
		}
		ctx.JSON(201, req)
	})
}
