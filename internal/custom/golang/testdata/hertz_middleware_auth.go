package main

import (
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/hertz-contrib/jwt"
)

func main() {
	h := server.Default()
	h.Use(RecoveryMiddleware(), AccessLog())
	authMw, _ := jwt.New(jwtMiddleware)
	h.Use(authMw.MiddlewareFunc())
	h.Spin()
}
