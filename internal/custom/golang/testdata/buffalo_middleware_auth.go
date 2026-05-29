package main

import (
	"github.com/gobuffalo/buffalo"
	"github.com/gobuffalo/mw-csrf"
)

func App() *buffalo.App {
	app := buffalo.New(buffalo.Options{})
	app.Use(csrf.New)
	app.Use(RequestLogger)
	app.Middleware.Use(JWTAuth)
	return app
}
