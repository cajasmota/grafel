use rusqlite::{Connection, params};

pub fn setup() -> rusqlite::Result<Connection> {
    let conn = Connection::open("app.db")?;
    conn.execute(
        "CREATE TABLE person (id INTEGER PRIMARY KEY, name TEXT NOT NULL, age INTEGER)",
        [],
    )?;
    Ok(conn)
}

pub fn insert_person(conn: &Connection, name: &str, age: i32) -> rusqlite::Result<()> {
    conn.execute(
        "INSERT INTO person (name, age) VALUES (?1, ?2)",
        params![name, age],
    )?;
    Ok(())
}

pub fn list_people(conn: &Connection) -> rusqlite::Result<()> {
    let mut stmt = conn.prepare("SELECT id, name, age FROM person WHERE age > ?1")?;
    let _rows = stmt.query_map([18], |row| row.get::<_, i32>(0))?;
    Ok(())
}
