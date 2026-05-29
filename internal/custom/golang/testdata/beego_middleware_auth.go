package main

import (
	"github.com/beego/beego/v2/server/web"
)

func init() {
	web.InsertFilter("/api/*", web.BeforeRouter, LoggingFilter)
	web.InsertFilter("/api/*", web.BeforeExec, JWTAuthFilter)
}

func main() {
	web.Run()
}
