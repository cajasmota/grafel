package main

import (
	"net/http"

	"github.com/gorilla/mux"
)

func main() {
	r := mux.NewRouter()
	r.Use(LoggingMiddleware)
	r.Use(JWTAuthMiddleware)
	http.ListenAndServe(":8080", r)
}
