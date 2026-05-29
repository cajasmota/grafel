package actions

import "github.com/gobuffalo/buffalo"

// App builds the Buffalo application and registers routes, a group, a mounted
// sub-app, a RESTful resource, and middleware.
func App() *buffalo.App {
	app := buffalo.New(buffalo.Options{})

	app.Use(SetCurrentUser)
	app.Use(Authorize)

	app.GET("/", HomeHandler)
	app.POST("/login", LoginHandler)

	api := app.Group("/api/v1")
	api.GET("/health", HealthHandler)

	app.Resource("/users", UsersResource{})

	app.Mount("/admin", admin.App())

	return app
}
