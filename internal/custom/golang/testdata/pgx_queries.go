package sample

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Product is scanned via pgx.RowToStructByName; db: tags map columns.
type Product struct {
	ID    int     `db:"id"`
	SKU   string  `db:"sku"`
	Price float64 `db:"price"`
}

func listProducts(ctx context.Context, pool *pgxpool.Pool) error {
	rows, err := pool.Query(ctx, "SELECT id, sku, price FROM products ORDER BY id")
	if err != nil {
		return err
	}
	defer rows.Close()
	return nil
}

func insertProduct(ctx context.Context, conn *pgx.Conn, p Product) error {
	_, err := conn.Exec(ctx, "INSERT INTO products (sku, price) VALUES ($1, $2)", p.SKU, p.Price)
	return err
}

func oneProduct(ctx context.Context, conn *pgx.Conn, id int) (Product, error) {
	var p Product
	err := conn.QueryRow(ctx, "SELECT id, sku, price FROM products WHERE id = $1", id).Scan(&p.ID, &p.SKU, &p.Price)
	return p, err
}
