package routers

import (
	"github.com/beego/beego/v2/server/web"
)

// UserController handles user resources.
type UserController struct {
	web.Controller
}

// GetAll lists users.
//
// @router /users [get]
func (c *UserController) GetAll() {}

// Post creates a user.
//
// @router /users [post]
func (c *UserController) Post() {}

func init() {
	// Method-style routing with an explicit verb:handler mapping.
	web.Router("/users", &UserController{}, "get:GetAll;post:Post")

	// RESTful default (no mapping string) -> ANY.
	web.Router("/health", &UserController{})

	// Namespace group with nested routers.
	ns := web.NewNamespace("/api/v1",
		web.NSRouter("/orders", &UserController{}, "get:GetAll"),
	)
	_ = ns

	// Auto-routing.
	web.AutoRouter(&UserController{})

	web.Run()
}
