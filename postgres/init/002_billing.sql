-- The order_id primary key stops a redelivered message from charging twice.
-- No foreign key to orders: billing only trusts what arrives on billing.queue.
CREATE TABLE IF NOT EXISTS billing_charges (
    order_id    VARCHAR(32) PRIMARY KEY,
    customer_id VARCHAR(32) NOT NULL,
    amount      BIGINT NOT NULL CHECK (amount > 0),
    charged_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
