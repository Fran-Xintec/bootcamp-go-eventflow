-- Claves de idempotencia de POST /orders: una clave solo puede apuntar a un pedido.
-- La clave primaria es lo que evita duplicados cuando llegan dos peticiones a la vez.
-- "request_hash" es la huella del cuerpo, para detectar la misma clave con otro cuerpo.
CREATE TABLE idempotency_keys (
    key          TEXT PRIMARY KEY CHECK (char_length(key) BETWEEN 1 AND 128),
    request_hash TEXT NOT NULL,
    order_id     UUID NOT NULL REFERENCES orders (id) ON DELETE CASCADE
);
