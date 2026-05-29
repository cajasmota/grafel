// Hertz routing fixture (CloudWeGo). Exercises engine creation, verb routes,
// nested route groups with prefix resolution, middleware, static mounts, and a
// NoRoute error handler — covering endpoint synthesis + handler attribution.
package hertzfixture

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
)

func register() {
	h := server.Default()

	h.Use(RecoveryMiddleware)

	h.GET("/", indexHandler)
	h.POST("/login", loginHandler)

	api := h.Group("/api/v1")
	api.Use(JWTAuth())
	api.GET("/users", listUsers)
	api.POST("/users", createUser)
	api.DELETE("/users/:id", deleteUser)

	admin := api.Group("/admin")
	admin.GET("/stats", adminStats)

	h.Static("/assets", "./static")

	h.NoRoute(notFoundHandler)

	h.Spin()
}

func indexHandler(ctx context.Context, c *app.RequestContext)    {}
func loginHandler(ctx context.Context, c *app.RequestContext)    {}
func listUsers(ctx context.Context, c *app.RequestContext)       {}
func createUser(ctx context.Context, c *app.RequestContext)      {}
func deleteUser(ctx context.Context, c *app.RequestContext)      {}
func adminStats(ctx context.Context, c *app.RequestContext)      {}
func notFoundHandler(ctx context.Context, c *app.RequestContext) {}
