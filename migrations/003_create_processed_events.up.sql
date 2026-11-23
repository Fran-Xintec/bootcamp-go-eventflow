-- Eventos que el worker ya procesó. Sirve para ignorar mensajes duplicados:
-- el broker entrega "al menos una vez", así que un mismo evento puede llegar repetido.
CREATE TABLE processed_events (
    event_id UUID PRIMARY KEY
);
