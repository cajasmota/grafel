// rbatis models and query fixture

use rbatis::rbatis::Rbatis;
use serde::{Deserialize, Serialize};

#[crud_table(table_name = "biz_activity")]
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct BizActivity {
    pub id: Option<String>,
    pub name: Option<String>,
    pub pc_link: Option<String>,
    pub delete_flag: Option<i32>,
}

#[crud_table(table_name = "user_info")]
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct UserInfo {
    pub id: Option<String>,
    pub username: Option<String>,
    pub email: Option<String>,
}

/// Query by py_sql
#[py_sql("select * from biz_activity where delete_flag = 0 and name like #{name}")]
async fn select_by_name(rb: &Rbatis, name: &str) -> Vec<BizActivity> {
    impled!()
}

/// Query by html_sql
#[html_sql]
async fn select_by_condition(rb: &Rbatis, id: &str) -> BizActivity {
    impled!()
}

/// Query by sql attribute (v4 style)
#[sql("select * from user_info where id = #{id}")]
async fn get_user_by_id(rb: &Rbatis, id: &str) -> UserInfo {
    impled!()
}

pub async fn init() {
    let rb = Rbatis::new();
    rb.link("mysql://root:123456@localhost:3306/test").await.unwrap();
}
