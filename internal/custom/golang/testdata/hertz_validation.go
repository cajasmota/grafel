package fixtures

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
)

// HertzCreateUserReq is a DTO bound + validated via hertz's built-in binder
// (c.Bind / c.BindAndValidate). hertz honours both vd: and binding: tags; the
// binding: tag is the surface this scanner recognises.
type HertzCreateUserReq struct {
	Name  string `json:"name" binding:"required"`
	Email string `json:"email" binding:"required,email"`
	Age   int    `json:"age" binding:"gte=0,lte=130"`
}

func newHertzValidationServer() {
	h := server.Default()
	h.POST("/users", func(ctx context.Context, c *app.RequestContext) {
		var req HertzCreateUserReq
		if err := c.Bind(&req); err != nil {
			c.JSON(400, map[string]any{"error": err.Error()})
			return
		}
		c.JSON(201, req)
	})
	h.Spin()
}
