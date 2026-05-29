package main

import (
	"github.com/kataras/iris/v12"
	"github.com/kataras/iris/v12/middleware/jwt"
	"github.com/kataras/iris/v12/middleware/logger"
)

func main() {
	app := iris.New()
	app.Use(logger.New())
	app.UseRouter(jwt.New(jwtConfig).VerifyMiddleware())
	v1 := app.Party("/v1")
	v1.UseGlobal(BasicAuth())
	app.Listen(":8080")
}
