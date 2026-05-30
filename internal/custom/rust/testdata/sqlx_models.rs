// SQLx models and migration fixture

use sqlx::FromRow;
use sqlx::PgPool;

#[derive(Debug, Clone, FromRow)]
pub struct User {
    pub id: i64,
    pub name: String,
    pub email: String,
}

#[derive(Debug, Clone, FromRow)]
pub struct Post {
    pub id: i64,
    pub user_id: i64,
    pub title: String,
    pub body: String,
}

pub async fn get_user(pool: &PgPool, id: i64) -> sqlx::Result<User> {
    sqlx::query_as!(User, "SELECT id, name, email FROM users WHERE id = $1", id)
        .fetch_one(pool)
        .await
}

pub async fn run_migrations(pool: &PgPool) -> sqlx::Result<()> {
    sqlx::migrate!("./migrations").run(pool).await?;
    Ok(())
}

pub async fn create_pool(url: &str) -> sqlx::Result<PgPool> {
    PgPool::connect(url).await
}
