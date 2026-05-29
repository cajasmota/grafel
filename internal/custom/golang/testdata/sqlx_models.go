package sample

import (
	"github.com/jmoiron/sqlx"
)

// User is scanned via sqlx StructScan; db: tags map columns.
type User struct {
	ID    int    `db:"id"`
	Name  string `db:"name"`
	Email string `db:"email,omitempty"`
	Skip  string `db:"-"`
}

// Order is another sqlx-mapped row struct.
type Order struct {
	ID     int     `db:"order_id"`
	UserID int     `db:"user_id"`
	Total  float64 `db:"total"`
}

func loadUsers(db *sqlx.DB) ([]User, error) {
	var users []User
	err := db.Select(&users, "SELECT id, name, email FROM users WHERE active = true")
	return users, err
}

func getUser(db *sqlx.DB, id int) (User, error) {
	var u User
	err := db.Get(&u, "SELECT id, name, email FROM users WHERE id = $1", id)
	return u, err
}

func insertUser(db *sqlx.DB, u User) error {
	_, err := db.NamedExec("INSERT INTO users (name, email) VALUES (:name, :email)", u)
	return err
}

const schemaDDL = `
CREATE TABLE IF NOT EXISTS orders (
    order_id INTEGER PRIMARY KEY,
    user_id  INTEGER NOT NULL,
    total    REAL,
    FOREIGN KEY (user_id) REFERENCES users(id)
)`
