import psycopg2

conn = psycopg2.connect("dbname=shop user=postgres")
cur = conn.cursor()

cur.execute(
    """
    CREATE TABLE IF NOT EXISTS accounts (
        id SERIAL PRIMARY KEY,
        owner_name TEXT NOT NULL,
        balance NUMERIC DEFAULT 0,
        opened_at TIMESTAMPTZ DEFAULT now()
    )
    """
)
