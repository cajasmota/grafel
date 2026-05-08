-- Source: synthetic dbt model based on common dbt patterns | License: MIT
-- dbt model: orders_model.sql

{{
  config(
    materialized='table',
    schema='analytics',
    tags=['orders', 'daily']
  )
}}

with orders as (
  select * from {{ source('jaffle_shop', 'orders') }}
),

customers as (
  select * from {{ ref('stg_customers') }}
),

payments as (
  select * from {{ ref('stg_payments') }}
),

final as (
  select
    orders.order_id,
    orders.customer_id,
    customers.first_name,
    customers.last_name,
    payments.payment_method,
    payments.amount
  from orders
  left join customers on orders.customer_id = customers.customer_id
  left join payments on orders.order_id = payments.order_id
)

select * from final
