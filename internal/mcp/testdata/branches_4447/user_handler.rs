// Representative branchy Rust handler for the #4447 branches-facet in-pipeline
// test. An axum-style handler exercising the distinctive Rust failure forms:
//   - std::env::var env-gate returning Err(StatusCode::SERVICE_UNAVAILABLE),
//   - an early-return guard returning Err(StatusCode::BAD_REQUEST),
//   - the `?` try operator propagating a lookup error,
//   - a validation guard returning Err(StatusCode::CONFLICT),
//   - a `match` Err(e) => arm returning Err(StatusCode::INTERNAL_SERVER_ERROR),
//   - a panic! guard.
use axum::{extract::{State, Json}, http::StatusCode};

pub async fn create_user(
    State(db): State<Db>,
    Json(payload): Json<NewUser>,
) -> Result<Json<User>, StatusCode> {
    if std::env::var("SIGNUP_ENABLED").is_err() {
        return Err(StatusCode::SERVICE_UNAVAILABLE);
    }
    if payload.email.is_empty() {
        return Err(StatusCode::BAD_REQUEST);
    }
    let existing = db.find_by_email(&payload.email).await?;
    if existing.is_some() {
        return Err(StatusCode::CONFLICT);
    }
    if db.is_poisoned() {
        panic!("db connection poisoned");
    }
    match db.insert(payload).await {
        Ok(u) => Ok(Json(u)),
        Err(e) => {
            tracing::error!("insert failed: {e}");
            return Err(StatusCode::INTERNAL_SERVER_ERROR);
        }
    }
}
