// Sample Actix-web application — golden fixture source.
use actix_web::{get, post, delete, web, App, HttpResponse, HttpServer, Responder};
use serde::{Deserialize, Serialize};
use std::sync::Mutex;

#[derive(Serialize, Deserialize, Clone)]
struct User {
    id: u32,
    name: String,
    email: String,
}

#[derive(Deserialize)]
struct CreateUser {
    name: String,
    email: String,
}

struct AppState {
    users: Mutex<Vec<User>>,
    next_id: Mutex<u32>,
}

#[get("/health")]
async fn health() -> impl Responder {
    HttpResponse::Ok().json(serde_json::json!({"status": "ok"}))
}

#[get("/users")]
async fn list_users(data: web::Data<AppState>) -> impl Responder {
    let users = data.users.lock().unwrap();
    HttpResponse::Ok().json(&*users)
}

#[get("/users/{id}")]
async fn get_user(path: web::Path<u32>, data: web::Data<AppState>) -> impl Responder {
    let id = path.into_inner();
    let users = data.users.lock().unwrap();
    match users.iter().find(|u| u.id == id) {
        Some(user) => HttpResponse::Ok().json(user),
        None => HttpResponse::NotFound().finish(),
    }
}

#[post("/users")]
async fn create_user(
    body: web::Json<CreateUser>,
    data: web::Data<AppState>,
) -> impl Responder {
    let mut next_id = data.next_id.lock().unwrap();
    let mut users = data.users.lock().unwrap();
    let user = User {
        id: *next_id,
        name: body.name.clone(),
        email: body.email.clone(),
    };
    *next_id += 1;
    users.push(user.clone());
    HttpResponse::Created().json(user)
}

#[delete("/users/{id}")]
async fn delete_user(path: web::Path<u32>, data: web::Data<AppState>) -> impl Responder {
    let id = path.into_inner();
    let mut users = data.users.lock().unwrap();
    let before = users.len();
    users.retain(|u| u.id != id);
    if users.len() < before {
        HttpResponse::NoContent().finish()
    } else {
        HttpResponse::NotFound().finish()
    }
}

#[actix_web::main]
async fn main() -> std::io::Result<()> {
    let state = web::Data::new(AppState {
        users: Mutex::new(vec![User {
            id: 1,
            name: "Alice".to_string(),
            email: "alice@example.com".to_string(),
        }]),
        next_id: Mutex::new(2),
    });

    HttpServer::new(move || {
        App::new()
            .app_data(state.clone())
            .service(health)
            .service(list_users)
            .service(get_user)
            .service(create_user)
            .service(delete_user)
    })
    .bind("0.0.0.0:8080")?
    .run()
    .await
}
