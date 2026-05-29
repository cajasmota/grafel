package fixtures

import (
	"github.com/beego/beego/v2/server/web"
)

// BeegoCreateUserReq is a beego-bound DTO using validate: struct tags.
type BeegoCreateUserReq struct {
	Name  string `form:"name" validate:"required,min=2,max=64"`
	Email string `form:"email" validate:"required,email"`
}

type BeegoValController struct {
	web.Controller
}

func (this *BeegoValController) Post() {
	var req BeegoCreateUserReq
	if err := this.ParseForm(&req); err != nil {
		this.Ctx.WriteString(err.Error())
		return
	}
	this.Ctx.WriteString("created")
}

func registerBeegoVal() {
	web.Router("/users", &BeegoValController{}, "post:Post")
}
