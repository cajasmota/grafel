package store

import "errors"

// User is the domain entity stored in memory.
type User struct {
	ID    string
	Email string
	Name  string
}

// Store is the read/write contract every backend must satisfy.
type Store interface {
	Get(id string) (User, error)
	List() []User
	Put(u User) error
}

// MemoryStore is an in-memory implementation of Store.
type MemoryStore struct {
	users map[string]User
}

// NewMemoryStore constructs an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{users: map[string]User{}}
}

// Get implements Store.
func (m *MemoryStore) Get(id string) (User, error) {
	u, ok := m.users[id]
	if !ok {
		return User{}, errors.New("not found")
	}
	return u, nil
}

// List implements Store.
func (m *MemoryStore) List() []User {
	out := make([]User, 0, len(m.users))
	for _, u := range m.users {
		out = append(out, u)
	}
	return out
}

// Put implements Store.
func (m *MemoryStore) Put(u User) error {
	m.users[u.ID] = u
	return nil
}
