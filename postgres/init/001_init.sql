CREATE TABLE IF NOT EXISTS products (
    sku          VARCHAR(32) PRIMARY KEY,
    name         TEXT NOT NULL,
    price        BIGINT NOT NULL CHECK (price >= 0),
    stock        INTEGER NOT NULL DEFAULT 0 CHECK (stock >= 0)
);

CREATE TABLE IF NOT EXISTS orders (
    order_id         VARCHAR(32) PRIMARY KEY,
    customer_id      VARCHAR(32) NOT NULL,
    total_amount     BIGINT NOT NULL CHECK (total_amount >= 0),
    shipping_address TEXT NOT NULL,
    status           VARCHAR(24) NOT NULL DEFAULT 'placed',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS order_items (
    order_id   VARCHAR(32) NOT NULL REFERENCES orders(order_id) ON DELETE CASCADE,
    sku        VARCHAR(32) NOT NULL REFERENCES products(sku),
    qty        INTEGER NOT NULL CHECK (qty > 0),
    unit_price BIGINT NOT NULL CHECK (unit_price >= 0),
    PRIMARY KEY (order_id, sku)
);

INSERT INTO products (sku, name, price, stock) VALUES
    ('PRD-01', 'Kopi Kilat', 25000, 100),
    ('PRD-02', 'Roti Ganesha', 15000, 80),
    ('PRD-03', 'Tumbler KilatMart', 40000, 30)
ON CONFLICT (sku) DO NOTHING;

INSERT INTO orders (order_id, customer_id, total_amount, shipping_address) VALUES
    ('ORD-1001', 'CUST-88', 50000, 'Jl. Ganesha No. 10, Bandung'),
    ('ORD-1002', 'CUST-21', 55000, 'Jl. Dago No. 15, Bandung'),
    ('ORD-1003', 'CUST-42', 80000, 'Jl. Dipatiukur No. 8, Bandung'),
    ('ORD-1004', 'CUST-17', 70000, 'Jl. Ciumbuleuit No. 20, Bandung'),
    ('ORD-1005', 'CUST-56', 70000, 'Jl. Setiabudi No. 30, Bandung')
ON CONFLICT (order_id) DO NOTHING;

INSERT INTO order_items (order_id, sku, qty, unit_price) VALUES
    ('ORD-1001', 'PRD-01', 2, 25000),
    ('ORD-1002', 'PRD-02', 1, 15000),
    ('ORD-1002', 'PRD-03', 1, 40000),
    ('ORD-1003', 'PRD-03', 2, 40000),
    ('ORD-1004', 'PRD-01', 1, 25000),
    ('ORD-1004', 'PRD-02', 3, 15000),
    ('ORD-1005', 'PRD-02', 2, 15000),
    ('ORD-1005', 'PRD-03', 1, 40000)
ON CONFLICT (order_id, sku) DO NOTHING;
