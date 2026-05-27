#[macro_use]
extern crate rocket;

#[get("/hello")]
fn hello() -> &'static str {
    "Hello, world!"
}

#[get("/users/<id>")]
fn show_user(id: u32) -> String {
    format!("user {}", id)
}

#[post("/users", data = "<body>")]
fn create_user(body: String) -> String {
    format!("created: {}", body)
}

#[launch]
fn rocket() -> _ {
    rocket::build().mount("/", routes![hello, show_user, create_user])
}
