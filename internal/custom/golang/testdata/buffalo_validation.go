package fixtures

import (
	"github.com/gobuffalo/buffalo"
)

// BuffaloOrderReq is a buffalo-bound DTO using validate: struct tags.
type BuffaloOrderReq struct {
	SKU      string `json:"sku" validate:"required,alphanum"`
	Quantity int    `json:"quantity" validate:"required,gte=1"`
}

func setupBuffaloVal() {
	app := buffalo.New(buffalo.Options{})
	app.POST("/orders", func(c buffalo.Context) error {
		var req BuffaloOrderReq
		if err := c.Bind(&req); err != nil {
			return c.Render(400, nil)
		}
		return c.Render(201, nil)
	})
}
