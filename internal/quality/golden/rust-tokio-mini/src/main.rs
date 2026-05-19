mod handler;
mod store;
mod worker;

use tokio::sync::mpsc;

use crate::handler::create_user;
use crate::store::MemoryStore;
use crate::worker::run_worker;

#[tokio::main]
async fn main() {
    let mut store = MemoryStore::new();
    create_user(&mut store, "u1".to_string(), "a@b.com".to_string()).await;

    let (_tx, rx) = mpsc::channel::<String>(8);
    run_worker(rx).await;
}
