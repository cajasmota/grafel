package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"example.com/demo/store"
)

// UsersHandler holds dependencies for /users routes.
type UsersHandler struct {
	Store store.Store
}

// NewUsersHandler wires a UsersHandler.
func NewUsersHandler(s store.Store) *UsersHandler {
	return &UsersHandler{Store: s}
}

// List returns all users as JSON.
func (h *UsersHandler) List(w http.ResponseWriter, r *http.Request) {
	users := h.Store.List()
	writeJSON(w, users)
}

// Get returns a single user by URL param.
func (h *UsersHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	u, err := h.Store.Get(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, u)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
