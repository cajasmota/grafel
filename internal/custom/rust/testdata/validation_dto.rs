// Validation and DTO extraction fixture

use serde::{Deserialize, Serialize};
use validator::Validate;
use actix_web::web;

/// DTO with serde Deserialize only
#[derive(Debug, Deserialize)]
pub struct CreateUserRequest {
    pub name: String,
    pub email: String,
}

/// DTO with serde + Validate
#[derive(Debug, Deserialize, Validate)]
pub struct UpdateUserRequest {
    #[validate(length(min = 1, max = 100))]
    pub name: String,
    #[validate(email)]
    pub email: String,
}

/// Axum-style handler with Json extractor
pub async fn create_user(
    web::Json(payload): web::Json<CreateUserRequest>,
) -> impl Responder {
    let req = payload;
    if req.validate().is_err() {
        return HttpResponse::BadRequest().finish();
    }
    HttpResponse::Ok().finish()
}

/// warp body json
pub fn routes() -> impl warp::Filter {
    warp::path("users")
        .and(warp::post())
        .and(warp::body::json())
        .and_then(create_user_handler)
}
