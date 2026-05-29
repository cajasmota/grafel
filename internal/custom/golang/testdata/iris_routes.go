package main

import (
	"github.com/kataras/iris/v12"
)

func main() {
	app := iris.New()

	app.Use(recoverMiddleware)

	app.Get("/health", healthCheck)
	app.Post("/login", login)

	// Party group with verb routes.
	v1 := app.Party("/api/v1")
	v1.Get("/users", listUsers)
	v1.Post("/users", createUser)

	// Nested party.
	admin := v1.Party("/admin")
	admin.Delete("/users/{id}", deleteUser)

	// Handle with explicit method.
	app.Handle("PUT", "/profile", updateProfile)

	app.Listen(":8080")
}

func recoverMiddleware(ctx iris.Context) {}
func healthCheck(ctx iris.Context)       {}
func login(ctx iris.Context)             {}
func listUsers(ctx iris.Context)         {}
func createUser(ctx iris.Context)        {}
func deleteUser(ctx iris.Context)        {}
func updateProfile(ctx iris.Context)     {}
