use diesel::prelude::*;

table! {
    users (id) {
        id -> Integer,
        name -> VarChar,
        email -> VarChar,
    }
}

table! {
    posts (id) {
        id -> Integer,
        title -> VarChar,
        user_id -> Integer,
    }
}

joinable!(posts -> users (user_id));

allow_tables_to_appear_in_same_query!(users, posts);
