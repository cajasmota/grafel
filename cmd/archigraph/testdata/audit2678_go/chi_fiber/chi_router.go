// Chi router file — registers routes pointing at handlers defined in
// handlers_chi.go.
package chifiber

import (
	"github.com/go-chi/chi/v5"
)

func setupChi() *chi.Mux {
	r := chi.NewRouter()
	r.Get("/orders", listOrders)
	r.Post("/orders", createOrder)
	return r
}
