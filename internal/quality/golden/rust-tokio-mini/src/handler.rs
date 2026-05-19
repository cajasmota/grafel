use crate::store::{MemoryStore, User, UserStore};

pub async fn create_user(store: &mut MemoryStore, id: String, email: String) {
    let u = User { id, email };
    store.put(u);
}

pub async fn fetch_user(store: &MemoryStore, id: &str) -> Option<User> {
    store.get(id)
}
