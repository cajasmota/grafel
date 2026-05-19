package main

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"example.com/demo/handlers"
	"example.com/demo/middleware"
	"example.com/demo/store"
)

func main() {
	s := store.NewMemoryStore()
	h := handlers.NewUsersHandler(s)

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Get("/users", h.List)
	r.Get("/users/{id}", h.Get)

	events := make(chan string, 8)
	runWorker(context.Background(), events)

	_ = http.ListenAndServe(":8080", r)
}
