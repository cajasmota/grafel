package fixtures

import (
	"github.com/beego/beego/v2/server/web"
	"github.com/go-playground/validator/v10"
)

// BeegoCreateUserReq is a DTO validated via go-playground validate: tags,
// bound inside a beego controller action.
type BeegoCreateUserReq struct {
	Name  string `json:"name" validate:"required,min=2,max=64"`
	Email string `json:"email" validate:"required,email"`
	Age   int    `json:"age" validate:"gte=0,lte=130"`
	Skip  string `json:"-" validate:"-"`
}

// BeegoUserValidationController embeds web.Controller.
type BeegoUserValidationController struct {
	web.Controller
}

func (c *BeegoUserValidationController) Post() {
	validate := validator.New()
	validate.RegisterValidation("is_even", func(fl validator.FieldLevel) bool {
		return fl.Field().Int()%2 == 0
	})

	var req BeegoCreateUserReq
	c.BindJSON(&req)
	if err := validate.Struct(&req); err != nil {
		c.Abort("400")
		return
	}
	c.Data["json"] = req
	c.ServeJSON()
}
