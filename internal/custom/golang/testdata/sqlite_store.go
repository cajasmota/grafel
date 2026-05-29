package sample

import (
	"database/sql"

	_ "github.com/mattn/go-sqlite3"
)

// Note is a row struct backed by the sqlite driver via database/sql.
type Note struct {
	ID   int    `db:"id"`
	Body string `db:"body"`
}

func initSchema(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS notes (
    id   INTEGER PRIMARY KEY AUTOINCREMENT,
    body TEXT NOT NULL,
    author_id INTEGER,
    FOREIGN KEY (author_id) REFERENCES authors(id)
)`)
	return err
}

func allNotes(db *sql.DB) (*sql.Rows, error) {
	return db.Query("SELECT id, body FROM notes")
}

func addNote(db *sql.DB, body string) error {
	_, err := db.Exec("INSERT INTO notes (body) VALUES (?)", body)
	return err
}
