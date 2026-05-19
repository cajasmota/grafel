use std::collections::HashMap;

#[derive(Debug, Clone)]
pub struct User {
    pub id: String,
    pub email: String,
}

pub trait UserStore {
    fn get(&self, id: &str) -> Option<User>;
    fn put(&mut self, user: User);
}

pub struct MemoryStore {
    users: HashMap<String, User>,
}

impl MemoryStore {
    pub fn new() -> Self {
        Self { users: HashMap::new() }
    }
}

impl UserStore for MemoryStore {
    fn get(&self, id: &str) -> Option<User> {
        self.users.get(id).cloned()
    }
    fn put(&mut self, user: User) {
        self.users.insert(user.id.clone(), user);
    }
}
