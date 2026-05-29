package store

import (
	"github.com/gocql/gocql"
)

const createUsers = `
CREATE TABLE IF NOT EXISTS shop.users (
	id uuid,
	name text,
	email text,
	created_at timestamp,
	PRIMARY KEY (id)
)`

const createOrders = `
CREATE TABLE orders (
	id uuid,
	user_id uuid,
	total decimal,
	PRIMARY KEY (id, user_id)
)`

func run(session *gocql.Session) error {
	if err := session.Query(`SELECT id, name FROM users WHERE id = ?`, "1").Exec(); err != nil {
		return err
	}
	if err := session.Query("INSERT INTO users (id, name) VALUES (?, ?)", "1", "Ada").Exec(); err != nil {
		return err
	}
	if err := session.Query(`UPDATE users SET name = ? WHERE id = ?`, "Bob", "1").Exec(); err != nil {
		return err
	}
	return session.Query(`DELETE FROM orders WHERE id = ?`, "1").Exec()
}
