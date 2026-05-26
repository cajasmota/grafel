// Package main — sample Go HTTP handler for golden fixture generation.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

// User represents a user entity.
type User struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// UserStore provides in-memory user storage.
type UserStore struct {
	users map[int]User
}

// NewUserStore creates an empty UserStore.
func NewUserStore() *UserStore {
	return &UserStore{users: make(map[int]User)}
}

// GetUser retrieves a user by ID.
func (s *UserStore) GetUser(id int) (User, bool) {
	u, ok := s.users[id]
	return u, ok
}

// SaveUser stores a user record.
func (s *UserStore) SaveUser(u User) {
	s.users[u.ID] = u
}

// handleHealth handles GET /health.
func handleHealth(w http.ResponseWriter, r *http.Request) {
	_ = r
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleGetUser handles GET /users/{id}.
func handleGetUser(store *UserStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var id int
		_, _ = fmt.Sscanf(r.URL.Query().Get("id"), "%d", &id)
		if id <= 0 {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		user, ok := store.GetUser(id)
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(user)
	}
}

func main() {
	store := NewUserStore()
	store.SaveUser(User{ID: 1, Name: "Alice", Email: "alice@example.com"})

	http.HandleFunc("/health", handleHealth)
	http.HandleFunc("/users", handleGetUser(store))

	log.Println("Listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
