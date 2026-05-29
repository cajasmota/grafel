package app

import (
	"github.com/revel/revel"
)

func init() {
	revel.InterceptFunc(RequestLogger, revel.BEFORE, revel.ALL_CONTROLLERS)
	revel.InterceptMethod(App.checkAuthUser, revel.BEFORE)
}

type App struct {
	*revel.Controller
}

func (c App) checkAuthUser() revel.Result {
	return nil
}
