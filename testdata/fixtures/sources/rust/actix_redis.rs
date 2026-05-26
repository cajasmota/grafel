// Rust Actix-web + Redis fixture.
// Demonstrates: HTTP endpoints, Redis pub/sub producer, Redis pub/sub consumer, DB access.
use actix_web::{get, post, delete, web, App, HttpResponse, HttpServer, Responder};
use redis::{Client, Commands, PubSubCommands};
use serde::{Deserialize, Serialize};
use sqlx::{PgPool, Row};
use std::sync::Arc;

#[derive(Serialize, Deserialize, Clone)]
struct Item {
    id: i32,
    name: String,
    value: f64,
}

#[derive(Deserialize)]
struct CreateItem {
    name: String,
    value: f64,
}

struct AppState {
    db: PgPool,
    redis: Arc<Client>,
}

#[get("/health")]
async fn health() -> impl Responder {
    HttpResponse::Ok().json(serde_json::json!({"status": "ok"}))
}

#[get("/items")]
async fn list_items(state: web::Data<AppState>) -> impl Responder {
    let rows = sqlx::query("SELECT id, name, value FROM items ORDER BY id")
        .fetch_all(&state.db)
        .await
        .unwrap_or_default();
    let items: Vec<Item> = rows
        .iter()
        .map(|r| Item {
            id: r.get("id"),
            name: r.get("name"),
            value: r.get("value"),
        })
        .collect();
    HttpResponse::Ok().json(items)
}

#[get("/items/{id}")]
async fn get_item(path: web::Path<i32>, state: web::Data<AppState>) -> impl Responder {
    let id = path.into_inner();
    match sqlx::query("SELECT id, name, value FROM items WHERE id = $1")
        .bind(id)
        .fetch_optional(&state.db)
        .await
    {
        Ok(Some(row)) => HttpResponse::Ok().json(Item {
            id: row.get("id"),
            name: row.get("name"),
            value: row.get("value"),
        }),
        _ => HttpResponse::NotFound().finish(),
    }
}

#[post("/items")]
async fn create_item(
    body: web::Json<CreateItem>,
    state: web::Data<AppState>,
) -> impl Responder {
    let row = sqlx::query(
        "INSERT INTO items (name, value) VALUES ($1, $2) RETURNING id, name, value",
    )
    .bind(&body.name)
    .bind(body.value)
    .fetch_one(&state.db)
    .await
    .unwrap();

    let item = Item {
        id: row.get("id"),
        name: row.get("name"),
        value: row.get("value"),
    };

    // Publish event to Redis channel
    let mut conn = state.redis.get_connection().unwrap();
    let payload = serde_json::to_string(&item).unwrap();
    let _: () = conn.publish("items:created", payload).unwrap();

    HttpResponse::Created().json(item)
}

#[delete("/items/{id}")]
async fn delete_item(path: web::Path<i32>, state: web::Data<AppState>) -> impl Responder {
    let id = path.into_inner();
    let result = sqlx::query("DELETE FROM items WHERE id = $1")
        .bind(id)
        .execute(&state.db)
        .await;
    match result {
        Ok(r) if r.rows_affected() > 0 => {
            let mut conn = state.redis.get_connection().unwrap();
            let _: () = conn.publish("items:deleted", id.to_string()).unwrap();
            HttpResponse::NoContent().finish()
        }
        _ => HttpResponse::NotFound().finish(),
    }
}

async fn redis_subscriber(client: Arc<Client>) {
    let mut conn = client.get_connection().unwrap();
    conn.subscribe(&["items:created", "items:deleted"], |msg| {
        let channel = msg.get_channel_name();
        let payload: String = msg.get_payload().unwrap_or_default();
        println!("Redis event on {}: {}", channel, payload);
        redis::ControlFlow::Continue
    })
    .unwrap();
}

#[actix_web::main]
async fn main() -> std::io::Result<()> {
    let pool = PgPool::connect("postgres://localhost/items_db").await.unwrap();
    let redis_client = Arc::new(Client::open("redis://127.0.0.1/").unwrap());

    let redis_clone = redis_client.clone();
    tokio::spawn(async move { redis_subscriber(redis_clone).await });

    let state = web::Data::new(AppState {
        db: pool,
        redis: redis_client,
    });

    HttpServer::new(move || {
        App::new()
            .app_data(state.clone())
            .service(health)
            .service(list_items)
            .service(get_item)
            .service(create_item)
            .service(delete_item)
    })
    .bind("0.0.0.0:8080")?
    .run()
    .await
}
