use diesel::prelude::*;
use crate::schema::{users, posts};

#[derive(Queryable, Identifiable, Selectable)]
#[diesel(table_name = users)]
pub struct User {
    pub id: i32,
    pub name: String,
    pub email: String,
}

#[derive(Insertable)]
#[diesel(table_name = users)]
pub struct NewUser<'a> {
    pub name: &'a str,
    pub email: &'a str,
}

#[derive(Queryable, Identifiable, Associations)]
#[belongs_to(User)]
#[diesel(table_name = posts)]
pub struct Post {
    pub id: i32,
    pub title: String,
    pub user_id: i32,
}

#[derive(AsChangeset)]
#[diesel(table_name = posts)]
pub struct UpdatePost {
    pub title: Option<String>,
}
