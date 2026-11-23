-- Pedidos y sus artículos. Los importes van en centavos (enteros).
-- No se guardan fechas: el enunciado no las pide para los pedidos.
CREATE TABLE orders (
    id          UUID PRIMARY KEY,
    customer_id TEXT   NOT NULL,
    status      TEXT   NOT NULL,
    total_cents BIGINT NOT NULL CHECK (total_cents > 0)
);

-- "position" conserva el orden en que llegaron los artículos.
CREATE TABLE order_items (
    order_id         UUID   NOT NULL REFERENCES orders (id) ON DELETE CASCADE,
    position         INT    NOT NULL,
    sku              TEXT   NOT NULL,
    quantity         INT    NOT NULL CHECK (quantity > 0),
    unit_price_cents BIGINT NOT NULL CHECK (unit_price_cents > 0),
    PRIMARY KEY (order_id, position)
);
