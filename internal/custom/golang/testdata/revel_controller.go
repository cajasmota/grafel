package controllers

import "github.com/revel/revel"

// App is a Revel controller embedding *revel.Controller.
type App struct {
	*revel.Controller
}

// Index renders the home page.
func (c App) Index() revel.Result {
	return c.Render()
}

// Login renders the login page.
func (c App) Login() revel.Result {
	return c.Render()
}

// checkUser is a before-filter interceptor.
func checkUser(c *revel.Controller) revel.Result {
	return nil
}

func init() {
	revel.InterceptFunc(checkUser, revel.BEFORE, &App{})
}
