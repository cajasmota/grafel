package main

import (
	"github.com/fasthttp/router"
	"github.com/valyala/fasthttp"
)

// listUsers is a raw fasthttp RequestHandler.
func listUsers(ctx *fasthttp.RequestCtx) {
	ctx.WriteString("users")
}

func main() {
	r := router.New()
	r.GET("/users", listUsers)
	r.POST("/users", createUser)
	r.Handle("PUT", "/users/{id}", updateUser)

	api := r.Group("/api/v1")
	api.GET("/health", healthCheck)

	fasthttp.ListenAndServe(":8080", r.Handler)
}
