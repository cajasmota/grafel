-- MySQL-flavored migration covering AUTO_INCREMENT and ENUM column types.

CREATE TABLE products (
    id INT NOT NULL AUTO_INCREMENT,
    sku VARCHAR(64) NOT NULL,
    status ENUM('active', 'archived', 'draft') NOT NULL DEFAULT 'draft',
    visibility ENUM('public', 'private') NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uniq_sku (sku)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE order_items (
    id BIGINT NOT NULL AUTO_INCREMENT,
    product_id INT NOT NULL,
    quantity INT NOT NULL,
    fulfillment ENUM('pending', 'shipped', 'delivered', 'returned') NOT NULL,
    PRIMARY KEY (id),
    CONSTRAINT fk_order_items_product FOREIGN KEY (product_id) REFERENCES products(id)
) ENGINE=InnoDB;
