import sqlite3

conn = sqlite3.connect("app.db")
cur = conn.cursor()

cur.executescript(
    """
    CREATE TABLE notes (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        title TEXT NOT NULL,
        body TEXT,
        pinned INTEGER DEFAULT 0
    );

    CREATE TABLE tags (
        id INTEGER PRIMARY KEY,
        label TEXT NOT NULL
    );
    """
)
