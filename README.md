# EventFlow

Servicio de pedidos en Go hecho para el desafío final del Bootcamp Go. La idea del desafío es empezar con una API sencilla e ir sumando confiabilidad, mensajería y operación por niveles. Este README crece con cada nivel: lo que ya está hecho no se borra, se le va agregando lo nuevo.

**Alcance.** Es un MVP: busca ser simple, funcional y correcto según lo que pide el enunciado, no un sistema de producción. Donde se tomó un atajo, queda dicho en "Decisiones" y en "Limitaciones conocidas".

## Avance por niveles

- [x] **Nivel 1: API modular** (RF-01 a RF-08): crear y consultar pedidos, validación, PostgreSQL con migraciones, errores consistentes, configuración por entorno, pruebas y ejecución con Docker.
- [x] **Nivel 2: Confiabilidad** (RF-05, RF-10, RF-14): clave de idempotencia, timeouts y límites, apagado ordenado.
- [x] **Nivel 3: Event-driven** (RF-09 a RF-12): RabbitMQ, eventos de pedido, worker con ACK/NACK, reintentos y cola de fallidos.
- [x] **Nivel 4: Operación** (RF-06, RF-13, RF-14): logs con correlación, métricas, health y readiness, documentación final.

### Qué se agregó en cada nivel

**Nivel 1 (hecho).** `POST /orders` y `GET /orders/{id}`; el dominio valida el pedido y calcula el total; los pedidos se guardan en PostgreSQL con migraciones versionadas; los errores salen siempre con el mismo formato y sin detalles internos; una notificación simulada se dispara en segundo plano sin hacer esperar la respuesta; todo se levanta con `docker compose up`; hay pruebas unitarias, HTTP e integración, y una colección de Postman.

**Nivel 2 (hecho).** `POST /orders` acepta la cabecera opcional `Idempotency-Key`: un reintento con la misma clave y el mismo cuerpo devuelve el pedido original sin crear otro, también cuando los reintentos llegan a la vez. Se agregaron un límite de 1 MiB para el cuerpo (413), timeouts del servidor HTTP, un plazo por petición contra la base (503 si se agota) y el apagado ordenado: al recibir SIGTERM o SIGINT se terminan las peticiones y las notificaciones en curso dentro de un plazo de 10 segundos, se cierra la base y el proceso sale con código 0. Los reintentos del cliente se simulan con pruebas (secuenciales y 20 peticiones simultáneas contra la base real) y con la carpeta "Nivel 2" de Postman.

**Nivel 3 (hecho).** Al crear un pedido la API publica el evento `order.created` en RabbitMQ (la notificación en memoria de los niveles anteriores se reemplazó por esto). Un worker independiente (`cmd/worker`) consume el evento, simula la notificación y pasa el pedido a `confirmed`. El mensaje se confirma (ACK) solo cuando el procesamiento terminó; un evento repetido se ignora; si el procesamiento falla se reintenta 3 veces con esperas de 1, 2 y 4 segundos y, si se agotan, el mensaje va a la cola de fallidos (`orders.created.dlq`). Si el broker no responde al crear el pedido, el pedido igual se guarda y se registra el error. Hay una demostración de fallos con Postman (ver "Mensajería y demostración de fallos").

**Nivel 4 (hecho).** Cada petición lleva un identificador de correlación (`X-Request-Id`: el del cliente o uno generado) que se devuelve en la respuesta, aparece en los logs estructurados de la API, en los errores (`request_id`), en el evento `order.created` y en los logs del worker, así que una misma operación se puede seguir de punta a punta. Se agregaron `GET /health/live`, `GET /health/ready` (la base responde) y `GET /metrics` (formato Prometheus, con total y duración de peticiones). El README tiene las respuestas a las preguntas del enunciado y la lista de limitaciones.

### Trazabilidad de requisitos

Cada requisito del enunciado con su nivel, su estado y dónde se puede comprobar. Se marca a medida que se cumplen.

| ID | Requisito (resumido) | Nivel | Estado | Dónde se cumple y cómo comprobarlo |
|---|---|---|---|---|
| RF-01 | Crear pedido con 201, id y estado inicial | 1 | [x] | `internal/orders/domain/entity/order.go` (`NewOrder`); `TestCreateOrder_Created`; Postman 1.1 |
| RF-02 | Validación con 4xx consistente y sin crear pedido | 1 | [x] | `TestNewOrder_Invalid`, `TestCreateOrder_InvalidRequests`; Postman 1.2 y 1.3 |
| RF-03 | Consultar pedido; 404 si no existe | 1 | [x] | `TestGetOrder`; Postman 1.1 y 1.4 |
| RF-04 | Persistencia, esquema y migraciones versionados | 1 | [x] | `migrations/001_create_orders.up.sql`, `pkg/database/migrate.go`; pruebas de integración; reiniciar la API y volver a consultar |
| RF-05 | Errores consistentes sin exponer detalles internos | 1 y 2 | [x] | `pkg/httputil/error_mapper.go`; `TestCreateOrder_InternalErrorDoesNotLeakDetails`; nivel 2: `TestCreateOrder_BodyTooLarge`, `TestCreateOrder_RequestTimeout`; Postman 2.2 |
| RF-06 | Configuración por entorno, sin secretos en el repo | 1 y 4 | [x] | `cmd/config/config.go`, `.env.example` (solo valores de ejemplo para desarrollo local), `.gitignore` excluye `.env` |
| RF-07 | Pruebas unitarias y HTTP de éxito y error | 1 | [x] | `go test ./...` (ver sección Pruebas) |
| RF-08 | Ejecución local reproducible con README | 1 | [x] | `docker-compose.yml` y la sección "Cómo levantarlo" |
| RF-09 | Evento al crear un pedido | 3 | [x] | `internal/orders/domain/event/order_created.go`, `internal/orders/infrastructure/rabbitmq.go`; `TestCreateOrder_PublishesEvent`, `TestRabbitMQ_PublishConsumeAndDuplicate`; Postman 3.1 |
| RF-10 | Idempotencia | 2 | [x] | `internal/orders/application/usecase/create_order.go`, `migrations/002_create_idempotency_keys.up.sql`; `TestCreateOrder_ConcurrentRetriesCreateOneOrder`, `TestCreateOrder_IdempotentRetry`; Postman 2.1 |
| RF-11 | Reintentos con límite y espera creciente | 3 | [x] | `internal/orders/infrastructure/rabbitmq_consumer.go`; `TestConsumer_RetriesTransientFailureThenAcks`, `TestConsumer_ExhaustedRetriesGoToDeadLetter`, `TestRabbitMQ_ExhaustedRetriesEndInDeadLetterQueue`; Postman 3.2 y 3.3 |
| RF-12 | Confirmación (ACK/NACK) y duplicados documentados | 3 | [x] | `rabbitmq_consumer.go`, `process_order_created.go`; `TestProcessOrderCreated_DuplicateEventIsIgnored`, `TestConsumer_AcksAfterSuccess`; ver "ACK/NACK y duplicados" |
| RF-13 | Logs estructurados con correlación | 4 | [x] | `pkg/middleware/request_id.go`, `access_log.go`; `TestRequestID_EchoedAndGenerated`, `TestProblem_IncludesRequestID`; Postman 4.1; ver "Correlación y observabilidad" |
| RF-14 | Apagado ordenado | 2 y 4 | [x] | API: `cmd/app/run.go`; worker: `cmd/worker/main.go`, `TestConsumer_ShutdownRequeuesMessage`; prueba manual con `docker compose stop api worker` (ver "Apagado ordenado") |

## Requisitos para correrlo

**Necesario:**

- Docker y Docker Compose (levantan PostgreSQL, RabbitMQ, la API y el worker). Conviene tener unos 2 GB de memoria libres para Docker.
- Los puertos 8080 (API), 5432 (PostgreSQL), 5672 y 15672 (RabbitMQ y su consola web) libres. Si alguno está ocupado se puede cambiar en `docker-compose.yml` o con un archivo `.env` (ver `.env.example`).
- Un navegador, para la consola de RabbitMQ (http://localhost:15672).

**Opcional:**

- Go 1.25 o superior, solo si se quieren correr las pruebas o la API fuera de Docker (el detector de carreras, `-race`, se corre dentro de Docker porque necesita gcc; ver "Pruebas automatizadas").
- Postman, para las pruebas manuales end-to-end (o Node, para correr la misma colección desde la terminal con `newman`).
- Un cliente de base de datos como DBeaver o pgAdmin, para ver las tablas (`orders`, `order_items`, `idempotency_keys` y `processed_events`); ver "Con un cliente de base de datos".
- `curl`, para los ejemplos de la línea de comandos.

No se necesita ninguna cuenta ni servicio de pago.

## Cómo levantarlo

Desde la carpeta `eventflow/`:

```bash
docker compose up --build
```

Esto levanta PostgreSQL, RabbitMQ, la API (puerto 8080) y el worker. La consola web de RabbitMQ queda en http://localhost:15672 (usuario y clave de ejemplo: `guest` / `guest`). Al arrancar, la API crea las tablas sola con las migraciones de la carpeta `migrations/`. No hace falta tener un `.env`: Compose trae valores de ejemplo para desarrollo. Para cambiar algo (puerto, espera de la notificación, etc.) se copia `.env.example` a `.env`.

Para dejarlo corriendo en segundo plano: `docker compose up -d --build`. Para apagar todo: `docker compose down`. Para borrar también los datos de la base: `docker compose down -v`.

## Cómo probar la API

### Con Postman (recomendado para probar end-to-end)

En la carpeta [`postman/`](postman/) hay una colección y un environment:

1. En Postman: Import, y elegir los dos archivos de `postman/` (`EventFlow.postman_collection.json` y `EventFlow.local.postman_environment.json`).
2. Arriba a la derecha, seleccionar el environment **EventFlow local** (trae `baseUrl` = `http://localhost:8080`).
3. Con todo levantado (`docker compose up -d`), ejecutar la colección entera con el Collection Runner, o las requests de a una. El environment también trae `rabbitUrl` (la consola de RabbitMQ), que usa una request del nivel 3. La colección completa tarda cerca de 30 segundos porque la carpeta del nivel 3 espera los reintentos del worker.

La colección se organiza con una carpeta por nivel del desafío ("Nivel N - nombre") y, dentro, una subcarpeta por escenario ("N.M"). Cada request trae sus pruebas automáticas (pestaña Tests). Cuando llega un nivel nuevo se agrega una carpeta nueva y las anteriores no se tocan, así que la colección completa sirve como regresión. Se ejecuta de arriba hacia abajo.

| Carpeta | Contenido |
|---|---|
| **Nivel 1 - API** | |
| 1.1 Crear pedido - happy path | Crear un pedido de dos artículos (verifica `201`, total y cabecera `Location`) y uno mínimo válido |
| 1.2 Crear pedido - cuerpo ilegible (400) | JSON roto, cuerpo vacío y tipo equivocado |
| 1.3 Crear pedido - validación (422) | `customer_id` y `items` ausentes o null, lista vacía, sin sku, cantidad y precio en 0 o negativos, valores sobre el máximo, más de 100 artículos |
| 1.4 Consultar pedido | Consulta del pedido recién creado (200), pedido que no existe (404) e id mal formado (400) |
| 1.5 Otros errores de protocolo (404, 405, 415) | Content-Type que no es JSON (415), ruta que no existe (404) y método no permitido (405) |
| **Nivel 2 - Confiabilidad** | |
| 2.1 Idempotencia (reintentos del cliente) | Clave nueva (201), reintento con la misma clave (200 e `Idempotent-Replayed`), misma clave con otro cuerpo (422), clave demasiado larga (400) y repetición sin clave (otro pedido) |
| 2.2 Límites de la petición | Cuerpo mayor a 1 MiB (413) |
| **Nivel 3 - Event-driven** | |
| 3.1 Evento y confirmación por el worker | Se crea el pedido y, unos segundos después, el worker lo dejó `confirmed` |
| 3.2 Fallo transitorio: reintentos | Cliente `demo-fallo-transitorio`: falla 2 veces, el worker reintenta (1 s y 2 s) y queda `confirmed` |
| 3.3 Fallo permanente: cola de fallidos | Cliente `demo-fallo-permanente`: se agotan los reintentos, el pedido sigue `created` y el mensaje queda en la DLQ (se consulta la API de RabbitMQ) |
| **Nivel 4 - Operación** | |
| 4.1 Correlación (X-Request-Id) | El id enviado vuelve en la respuesta, los errores llevan `request_id` y, si no se envía, se genera uno |
| 4.2 Health y métricas | `/health/live`, `/health/ready` y `/metrics` |

Si se tiene Node instalado, también se puede correr desde la terminal: `npx newman run postman/EventFlow.postman_collection.json -e postman/EventFlow.local.postman_environment.json`.

### Con curl

Crear un pedido (los precios van en centavos: 1500 equivale a $15,00):

```bash
curl -i -X POST localhost:8080/orders \
  -H 'Content-Type: application/json' \
  -d '{"customer_id":"cliente-1","items":[{"sku":"A-1","quantity":2,"unit_price_cents":1500},{"sku":"B-2","quantity":1,"unit_price_cents":999}]}'
```

Respuesta (`201 Created`, con la cabecera `Location: /orders/<id>` que apunta al pedido nuevo):

```json
{"id":"4f8dab67-0750-47f7-8187-30578d19e63e","status":"created","total_cents":3999}
```

Consultar el pedido con el id que devolvió:

```bash
curl -i localhost:8080/orders/4f8dab67-0750-47f7-8187-30578d19e63e
```

Para repetir el pedido sin crear un duplicado se envía la cabecera `Idempotency-Key` (ver "Qué pasa si el cliente repite el POST" más abajo).

En PowerShell conviene usar `curl.exe` (en lugar de `curl`) y poner el JSON en un archivo con `-d @pedido.json`, porque las comillas se complican.

### Con un cliente de base de datos (DBeaver, pgAdmin, etc.)

Como Compose publica el puerto de PostgreSQL, sirve cualquier cliente. En DBeaver: Database, New Database Connection, PostgreSQL, y completar:

| Campo | Valor |
|---|---|
| Host | `localhost` |
| Port | `5432` (o el `DB_PORT` del `.env`) |
| Database | `eventflow` |
| Username | `eventflow` |
| Password | `eventflow` |

La primera vez DBeaver ofrece descargar el driver de PostgreSQL. Con Test Connection se comprueba y después se ven las tablas en `eventflow`, `Schemas`, `public`, `Tables`: `orders` (con su `status`: `created` o `confirmed`), `order_items`, `idempotency_keys` (claves de idempotencia y el pedido que crearon), `processed_events` (eventos que el worker ya procesó) y `schema_migrations` (las migraciones ya aplicadas). Estos valores son solo de ejemplo para desarrollo local. Si ya hay otro PostgreSQL usando el puerto 5432, se cambia `DB_PORT` en `.env` y se vuelve a levantar.

### Otras formas de revisar

- `docker compose logs api`: logs de la API, con el detalle real de los errores 500 y los avisos si falla la publicación del evento. `docker compose logs worker`: logs del worker (notificaciones, reintentos y mensajes enviados a la cola de fallidos).
- `docker compose exec db psql -U eventflow -d eventflow -c "SELECT * FROM orders;"`: mirar lo que quedó guardado.

### Qué pasa si el cliente repite el POST (idempotencia)

Si el cliente no recibe la respuesta (corte de red, timeout) no puede saber si el pedido se creó, y reintentar podría duplicarlo. Para eso `POST /orders` acepta la cabecera opcional `Idempotency-Key` (entre 1 y 128 caracteres). El cliente la genera una sola vez por pedido, por ejemplo un UUID, y la reutiliza en todos los reintentos.

```bash
curl -i -X POST localhost:8080/orders \
  -H 'Content-Type: application/json' -H 'Idempotency-Key: 9e4d2371-4b8e-4d15-9a02-c1e5f8b7d340' \
  -d '{"customer_id":"cliente-1","items":[{"sku":"A-1","quantity":2,"unit_price_cents":1500}]}'
```

| Situación | Resultado |
|---|---|
| Clave nueva | 201 y el pedido se crea |
| Misma clave y mismo cuerpo | 200 con el pedido original y la cabecera `Idempotent-Replayed: true`; no se crea otro ni se vuelve a notificar |
| Misma clave y cuerpo distinto | 422 `IDEMPOTENCY_KEY_REUSED`; no se crea nada |
| Clave vacía o de más de 128 caracteres | 400 `IDEMPOTENCY_KEY_INVALID` |
| Sin cabecera | 201 y se crea un pedido nuevo cada vez: la idempotencia es opcional y depende del cliente |

Cómo funciona: la clave se guarda en la tabla `idempotency_keys` con una restricción única, en la misma transacción que el pedido. Si dos peticiones con la misma clave llegan a la vez, la base deja pasar solo a una; la otra falla al guardar la clave y responde como un reintento. Se compara el cuerpo con una huella SHA-256 del contenido (no de los bytes, así que el espaciado del JSON no importa). Hay pruebas de este caso con 20 peticiones simultáneas contra la base real.

### Apagado ordenado

Al recibir SIGTERM o SIGINT (por ejemplo `docker compose stop` o Ctrl+C) la API deja de aceptar peticiones nuevas, espera las que están en curso (la publicación del evento ocurre dentro de la petición), cierra la conexión a RabbitMQ y a la base y termina con código 0. El worker deja de tomar mensajes y termina el que tiene en curso; si estaba esperando un reintento, devuelve el mensaje a la cola para que no se pierda. El plazo total es `SHUTDOWN_TIMEOUT` (10 segundos por defecto); si vence, queda registrado en el log y el proceso termina con un código distinto de 0.

Para comprobarlo a mano: apagar el worker mientras procesa y ver cómo termina.

```bash
docker compose stop api worker
docker compose logs worker --tail 5   # "Terminando worker..." y "Worker terminado correctamente"
docker compose ps -a                  # la API y el worker figuran como Exited (0)
```

### Mensajería y demostración de fallos

Flujo normal: la API guarda el pedido (`status: created`) y publica `order.created`; el worker lo consume, simula la notificación (200 ms) y deja el pedido `confirmed`. Se puede ver la consola de RabbitMQ en http://localhost:15672 (`guest` / `guest`), en Queues: `orders.created` (cola principal) y `orders.created.dlq` (cola de fallidos).

El evento que viaja por el broker:

```json
{"id": "c65eb19a-e507-497b-b4c0-f08c3baf0f16", "type": "order.created", "occurred_at": "2026-11-23T01:20:02Z", "order_id": "163fa0ff-d710-4285-bef4-5c8fcdeb55e9"}
```

**ACK/NACK y duplicados (política):**

| Situación | Qué hace el worker |
|---|---|
| Procesó bien | ACK |
| Falla (por ejemplo, la notificación) | Reintenta tras 1 s, 2 s y 4 s (hasta 4 intentos en total) sin confirmar el mensaje |
| Se agotan los reintentos | NACK sin reencolar: RabbitMQ lo manda a `orders.created.dlq` y queda un log de nivel `error` |
| El mensaje no es un evento válido | NACK directo a la DLQ, sin reintentos |
| Llega un evento con un `id` ya procesado | Lo ignora y hace ACK (no repite la notificación ni el cambio de estado) |
| Se apaga el worker durante una espera | Devuelve el mensaje a la cola (no se pierde) |

**Escenarios de fallo que se pueden demostrar** (la carpeta "Nivel 3" de Postman los ejecuta; también se pueden hacer a mano con `curl`):

1. **Fallo transitorio.** Crear un pedido con `"customer_id": "demo-fallo-transitorio"`. La notificación simulada falla las 2 primeras veces. En `docker compose logs worker` se ven dos avisos de reintento (1 s y 2 s) y al tercer intento el pedido queda `confirmed`.
2. **Fallo permanente.** Crear un pedido con `"customer_id": "demo-fallo-permanente"`. Tras 3 reintentos (1 s, 2 s y 4 s) el log muestra un `error` ("se agotaron los reintentos"), el pedido sigue `created` y el mensaje aparece en `orders.created.dlq`.
3. **Broker caído.** `docker compose stop rabbitmq` y crear un pedido: responde `201` igual (tarda unos segundos por los 3 intentos) y la API registra el error de publicación. Con `docker compose start rabbitmq` todo vuelve a funcionar.
4. **Mensaje duplicado.** En la consola de RabbitMQ, Exchanges, `orders`, "Publish message" con la clave de enrutamiento `order.created` y el mismo cuerpo dos veces (con un `order_id` de un pedido existente): el worker hace el cambio una sola vez y el segundo se registra como "evento duplicado, se ignora". También lo cubre `TestRabbitMQ_PublishConsumeAndDuplicate`.

Para revisar los datos en un cliente de base de datos (DBeaver, ver arriba): la tabla `orders` muestra `status` = `confirmed` o `created`, y `processed_events` tiene una fila por cada evento procesado.

**Salida real de los dos escenarios principales** (`docker compose logs worker`, recortada; los pedidos se crearon con `X-Request-Id` `demo-transitorio` y `demo-permanente`):

```text
[WARN]  falló el procesamiento del evento, se reintenta  {"request_id": "demo-transitorio", "retry": 1, "wait": "1s", "error": "fallo simulado transitorio del servicio de notificaciones"}
[WARN]  falló el procesamiento del evento, se reintenta  {"request_id": "demo-transitorio", "retry": 2, "wait": "2s", "error": "fallo simulado transitorio del servicio de notificaciones"}
[INFO]  notificación simulada enviada                    {"request_id": "demo-transitorio", "order_id": "3b54faa4-..."}
[INFO]  pedido confirmado                                {"request_id": "demo-transitorio", "event_id": "b60d143b-...", "order_id": "3b54faa4-..."}

[WARN]  falló el procesamiento del evento, se reintenta  {"request_id": "demo-permanente", "retry": 1, "wait": "1s", "error": "fallo simulado permanente del servicio de notificaciones"}
[WARN]  falló el procesamiento del evento, se reintenta  {"request_id": "demo-permanente", "retry": 2, "wait": "2s", ...}
[WARN]  falló el procesamiento del evento, se reintenta  {"request_id": "demo-permanente", "retry": 3, "wait": "4s", ...}
[ERROR] se agotaron los reintentos, el mensaje va a la cola de fallidos  {"request_id": "demo-permanente", "order_id": "ef8d49e5-...", "attempts": 4, ...}
```

Resultado: el pedido del fallo transitorio terminó `confirmed`; el del fallo permanente sigue `created` y su mensaje quedó en `orders.created.dlq` (se ve en la consola de RabbitMQ).


### Correlación y observabilidad

- **Logs estructurados.** La API escribe un log por petición (`request_id`, método, ruta, estado y duración en milisegundos) y el worker escribe los suyos con los mismos campos. En el entorno `dev` salen en consola con colores; con `ENVIRONMENT=prod` salen en JSON, uno por línea. No se registran cuerpos de petición ni datos del cliente (solo ids).
- **Seguir una operación.** Se envía un id propio (si no, la API genera uno) y se busca en los logs de los dos procesos:

```bash
curl -s -o /dev/null -X POST localhost:8080/orders -H 'Content-Type: application/json' -H 'X-Request-Id: demo-1' \
  -d '{"customer_id":"cliente-1","items":[{"sku":"A-1","quantity":1,"unit_price_cents":100}]}'
docker compose logs api worker | grep demo-1     # sale la petición en la API y la notificación y confirmación en el worker
```

- **Health.** `GET /health/live` responde 200 si el proceso funciona. `GET /health/ready` responde 200 si la base de datos responde y 503 si no. RabbitMQ no cuenta en `ready` a propósito: la API sigue siendo útil sin el broker.
- **Métricas.** `GET /metrics` entrega en formato Prometheus `http_requests_total` (por método, ruta registrada y estado) y `http_request_duration_seconds`. No se instrumentó el worker.


### Casos posibles y sus códigos

El contrato completo está en [`docs/openapi.yaml`](docs/openapi.yaml). Los códigos siguen la semántica de HTTP (RFC 9110):

| Caso | Ejemplo | Código | Por qué ese código |
|---|---|---|---|
| Pedido creado | POST con cliente y artículos válidos | 201 Created | Se creó un recurso nuevo (RFC 9110, 15.3.2) |
| Pedido encontrado | GET con un id existente | 200 OK | La consulta salió bien |
| JSON roto o con tipos equivocados | `{"customer_id": ` o `"quantity": "dos"` | 400 Bad Request | La petición no se puede interpretar: sintaxis o tipos inválidos (RFC 9110, 15.5.1) |
| Datos inválidos | sin cliente, lista vacía, cantidad o precio menores o iguales a 0, más de 100 artículos | 422 Unprocessable Content | El JSON se entiende pero incumple una regla del pedido (RFC 9110, 15.5.21). El enunciado solo pide un 4xx consistente; se prefirió distinguir "no se pudo leer" (400) de "se leyó pero no cumple las reglas" (422) |
| Id que no es un UUID | `GET /orders/abc` | 400 Bad Request | El id viene mal formado (error de sintaxis de la URL), no es un pedido que "no existe" |
| Content-Type que no es JSON | POST con `text/plain` | 415 Unsupported Media Type | El cuerpo no viene en el formato que la API acepta (RFC 9110, 15.5.16) |
| Pedido que no existe | `GET /orders/<UUID válido sin pedido>` | 404 Not Found | El recurso no existe (RFC 9110, 15.5.5) |
| Ruta o método inexistente | `GET /nada`, `DELETE /orders` | 404 Not Found / 405 Method Not Allowed | La ruta no existe (RFC 9110, 15.5.5) o no admite ese método (15.5.6; el 405 incluye la cabecera `Allow` con los métodos permitidos). Se responden con el mismo formato de error en vez del texto plano por defecto del framework |
| Cuerpo mayor a 1 MiB | POST con un cuerpo de más de 1 MiB | 413 Content Too Large | El cuerpo supera el límite que acepta el servidor (RFC 9110, 15.5.14) |
| Clave de idempotencia mal formada | `Idempotency-Key` vacía o de más de 128 caracteres | 400 Bad Request | La cabecera no se puede interpretar (RFC 9110, 15.5.1) |
| Clave de idempotencia con otro cuerpo | misma clave, pedido distinto | 422 Unprocessable Content | Se entiende pero contradice la petición original (RFC 9110, 15.5.21) |
| Plazo de la petición agotado | la base tarda más de `REQUEST_TIMEOUT_MS` | 503 Service Unavailable | El servicio no pudo responder a tiempo; el cliente puede reintentar (RFC 9110, 15.6.4) |
| Falla interna | la base de datos cae | 500 Internal Server Error | Es culpa del servidor, no del cliente (RFC 9110, 15.6.1). El mensaje es genérico a propósito: no se exponen SQL ni credenciales (recomendación de OWASP contra la filtración de información) |

Todos los errores siguen el RFC 9457 (Problem Details for HTTP APIs) y se responden con `Content-Type: application/problem+json`:

```json
{
  "type": "/problems/ORDER_ITEMS_REQUIRED",
  "title": "Unprocessable Entity",
  "status": 422,
  "detail": "se requiere al menos un artículo",
  "instance": "/orders",
  "code": "ORDER_ITEMS_REQUIRED"
}
```

`title` es la frase estándar del código HTTP (en inglés, como define el estándar), `detail` el mensaje en español para quien lo lee, `instance` la ruta de la petición y `code` una extensión: un identificador estable para que un programa decida qué hacer. El catálogo completo de códigos está al inicio de [`docs/openapi.yaml`](docs/openapi.yaml).

## Pruebas automatizadas

```bash
go test ./...                     # unitarias y HTTP, no necesitan base de datos
```

Las pruebas de integración usan la base y RabbitMQ del Compose y se saltan solas si no hay `DB_HOST` o `RABBITMQ_URL`:

```bash
docker compose up -d db rabbitmq
DB_HOST=localhost RABBITMQ_URL=amqp://guest:guest@localhost:5672/ go test -count=1 ./...
```

(En PowerShell: `$env:DB_HOST="localhost"; $env:RABBITMQ_URL="amqp://guest:guest@localhost:5672/"; go test -count=1 ./...`.)

Para el detector de carreras (`-race`) hace falta gcc, que no está instalado en el entorno de desarrollo (Windows), por lo que se ejecuta dentro de la imagen oficial de Go:

```bash
docker run --rm -v "$PWD":/app -w /app golang:1.25 go test -race ./...
```

(En PowerShell: `docker run --rm -v "${PWD}:/app" -w /app golang:1.25 go test -race ./...`.)

Qué se prueba, por capas:

| Capa | Qué se comprueba | Cómo |
|---|---|---|
| Dominio | Validación del pedido (cliente vacío, lista vacía, cantidades y precios inválidos, límites) y cálculo del total | Table-driven tests |
| Casos de uso | Crear y consultar con un repositorio fake; que el repositorio no se llame si el pedido es inválido; que se publique el evento y que un fallo del broker no afecte la respuesta; idempotencia (reintento, otro cuerpo, clave inválida, carrera al guardar); procesamiento del evento (confirma, ignora duplicados, no confirma si la notificación falla) | Fakes hechos a mano |
| HTTP | Códigos 201, 400, 404, 405, 413, 415, 422, 500 y 503, formato del error, reintentos con `Idempotency-Key` y que no se filtren detalles internos | `httptest` sobre el router real |
| Persistencia | Guardar y leer en PostgreSQL, pedido inexistente, transacción atómica, migraciones repetidas, claves de idempotencia y 20 reintentos simultáneos que deben dejar un solo pedido | Integración con la base del Compose |
| Mensajería | Consumidor: ACK tras procesar, reintentos con espera creciente, NACK a la cola de fallidos al agotarlos, mensaje inválido directo a la DLQ y reencolado al apagar. Con RabbitMQ real: publicar, consumir y confirmar el pedido, evento duplicado, llegada a la DLQ y broker caído | Pruebas con un ACK falso y de integración con el Compose |
| End-to-end (e2e) | Todos los casos de la tabla de códigos, la idempotencia, los escenarios de fallo del worker, la correlación y el health, contra todo el sistema levantado | Colección de Postman |

### Evidencia de ejecución

Resultado de la última ejecución completa (2026-11-23), con PostgreSQL y RabbitMQ del Compose levantados desde cero (`docker compose down -v` y `docker compose up -d --build`):

| Comprobación | Comando | Resultado |
|---|---|---|
| Análisis estático | `go vet ./...` | sin hallazgos |
| Pruebas con detector de carreras, incluida la integración con PostgreSQL y RabbitMQ reales | `go test -race -count=1 ./...` (dentro de `golang:1.25`, con `DB_HOST` y `RABBITMQ_URL`) | `ok` en todos los paquetes: `cmd/routes`, `usecase`, `entity`, `infrastructure`, `handler` y `httputil` |
| Colección de Postman completa (niveles 1 a 4) | `newman run postman/EventFlow.postman_collection.json -e postman/EventFlow.local.postman_environment.json` | 43 requests y 109 aserciones, 0 fallos |
| Contrato OpenAPI | `redocly lint docs/openapi.yaml` | válido |

## Cómo está organizado

```mermaid
flowchart LR
    Cliente["Cliente HTTP"] --> Handler["Handler (Gin)"]
    Handler --> UC["Casos de uso<br/>CreateOrder / GetOrder"]
    UC --> Dominio["Dominio<br/>Order, validación, total"]
    UC -->|interfaz OrderRepository| Repo["Repositorio PostgreSQL"]
    UC -->|interfaz EventPublisher| Pub["Publicador RabbitMQ"]
    Repo --> DB[("PostgreSQL<br/>orders, order_items,<br/>idempotency_keys,<br/>processed_events")]
    Pub -->|order.created| MQ{{"RabbitMQ<br/>orders.created"}}
    MQ --> Worker["Worker<br/>(cmd/worker)"]
    Worker -->|ACK al terminar| MQ
    MQ -->|reintentos agotados| DLQ{{"orders.created.dlq"}}
    Worker -->|notifica y confirma el pedido| DB
    Cliente -.->|/health, /metrics| Ops["Rutas operativas<br/>health live y ready, métricas"]
    Ops -.-> DB
```

Flujo de una creación de pedido, con el camino de reintentos y de fallos del worker:

```mermaid
sequenceDiagram
    participant C as Cliente
    participant API as API
    participant DB as PostgreSQL
    participant MQ as RabbitMQ
    participant W as Worker

    C->>API: POST /orders (Idempotency-Key, X-Request-Id)
    API->>DB: guarda pedido + clave de idempotencia (una transacción)
    API->>MQ: publica order.created (con confirmación del broker)
    API-->>C: 201 Created (+ Location)
    MQ->>W: entrega el evento
    alt el evento ya estaba procesado
        W-->>MQ: ACK (duplicado ignorado)
    else procesa bien
        W->>W: notifica (simulado)
        W->>DB: registra el evento y pasa el pedido a confirmed (una transacción)
        W-->>MQ: ACK
    else falla
        W->>W: reintenta tras 1 s, 2 s y 4 s
        W-->>MQ: NACK sin reencolar (agotados los reintentos)
        MQ->>MQ: el mensaje pasa a orders.created.dlq
    end
    Note over API,MQ: si el broker no responde, el pedido queda guardado,<br/>se registra el error y el cliente igual recibe 201
```

```text
cmd/            arranque: api (main), worker (consumidor), app (ciclo de vida), config, container (inyección), routes
internal/
  orders/       módulo de pedidos: domain, application, infrastructure, presentation
  notifications/ notificador simulado (lo usa el worker)
pkg/            utilidades compartidas: errores, base de datos y migraciones, logger, middleware
migrations/     SQL versionado (001_create_orders, 002_create_idempotency_keys, 003_create_processed_events)
docs/           contrato OpenAPI
postman/        colección y environment para pruebas manuales
go.mod, go.sum  módulo de Go y sus dependencias (go.sum guarda sus checksums)
```

Las dependencias apuntan hacia adentro: el dominio no conoce a Gin ni a PostgreSQL, y los casos de uso solo conocen interfaces. Es la estructura de la semilla del bootcamp, que se conservó porque deja claro dónde va cada cosa.

## Decisiones y por qué

- **Se partió de la semilla, pero se le quitaron cosas.** Se sacaron el proveedor de mensajería en la nube, Swagger y el módulo `health` de ejemplo, porque el desafío no los pide y traían muchas dependencias. Se mantuvieron la estructura por módulos y capas, el manejo de errores y el middleware.
- **Las reglas de negocio están en el dominio.** `NewOrder` valida y calcula el total; el handler solo lee el JSON. Así las reglas se pueden probar sin levantar HTTP.
- **Los importes son enteros en centavos.** Con `float64`, `0.1 + 0.2` da `0.30000000000000004`, y en un total de pedido eso es un error real. Con enteros las sumas son exactas.
- **Los errores se traducen en un solo lugar.** Un mapper convierte los errores de dominio en códigos HTTP usando `errors.As` (funciona aunque el error venga envuelto). Cualquier otro error responde 500 genérico y el detalle real queda solo en el log.
- **Migraciones propias y pequeñas.** Los archivos `.sql` van embebidos en el binario y un aplicador pequeño los corre en orden al arrancar (la API y el worker, con un candado en la base para que no choquen), guardando cuáles ya aplicó. Se descartó `golang-migrate` para no sumar una dependencia por algo tan chico, y `docker-entrypoint-initdb.d` porque solo corre cuando el volumen está vacío y el esquema crece con cada nivel.
- **Las tablas no guardan fechas.** El enunciado no las pide para los pedidos, así que no se agregaron.
- **La notificación se movió a un worker con RabbitMQ (nivel 3).** En los niveles 1 y 2 era una goroutine dentro de la API, que se perdía si el proceso caía y no se podía reintentar. Ahora la API solo publica el evento `order.created` y un proceso aparte lo consume. Se eligió RabbitMQ (el broker que sugiere el enunciado) y no Kafka o NATS para no sumar piezas.
- **El contrato OpenAPI se escribió a mano.** Se evitó generarlo con Swagger porque pide una herramienta extra y anotaciones en cada handler, y el archivo generado se desactualiza fácil.
- **La idempotencia es opcional y se resuelve con la base.** Se guarda `(clave, huella del cuerpo, pedido)` con clave primaria en la misma transacción que el pedido; esa restricción única resuelve la carrera entre peticiones simultáneas sin candados extra. Se descartó una columna única en `orders` porque mezcla un detalle del transporte con la entidad. Un reintento responde `200` y no `201`: `201` significa que esa petición creó un recurso y el reintento no crea nada. Lleva el mismo cuerpo que la primera respuesta y la cabecera `Idempotent-Replayed: true`. Otras APIs repiten el `201` original; se prefirió `200` por ser la semántica exacta de HTTP (RFC 9110, 15.3.1 y 15.3.2).
- **Los errores siguen el RFC 9457.** Se usó el estándar (`application/problem+json`) en vez de un formato propio, con una extensión `code` estable. Es un poco más verboso, pero cualquier cliente o herramienta que conozca el estándar lo entiende.
- **No hay estado "en curso" para las claves.** Como la clave y el pedido se guardan en una sola transacción, una petición simultánea espera a que la primera termine y luego responde como reintento; si el proceso se cae a la mitad, la transacción se deshace y la clave queda libre.
- **Plazos en tres niveles.** El servidor HTTP tiene timeouts de lectura, escritura e inactividad (protegen de clientes lentos); cada petición tiene un plazo (`REQUEST_TIMEOUT_MS`) que cancela la consulta a la base; y el apagado tiene su propio plazo (`SHUTDOWN_TIMEOUT`). Se dejó fijo el valor de los timeouts del servidor y configurables los otros dos.
- **El apagado de la API espera las peticiones en curso** (la publicación del evento ocurre dentro de la petición); el del worker termina el mensaje que tiene o lo devuelve a la cola.
- **El evento se publica después de guardar el pedido** (Q-03). La publicación usa confirmaciones del broker y 3 intentos; si el broker no responde, el pedido queda guardado, se registra el error con el `order_id` y el cliente igual recibe `201`. La alternativa más segura es un outbox transaccional (guardar el evento en la misma transacción y publicarlo aparte), que el enunciado nombra como extensión; no se implementó para mantener el MVP simple y queda como mejora.
- **ACK solo cuando el procesamiento terminó.** El worker hace ACK después de notificar y de confirmar el pedido. Un fallo no confirma el mensaje: se reintenta y, al agotar los reintentos, se rechaza sin reencolar (NACK) y RabbitMQ lo pasa a la cola de fallidos.
- **Reintentos dentro del worker.** Intento inicial y 3 reintentos con esperas de 1, 2 y 4 segundos. Se descartaron las colas de espera con TTL o el plugin de mensajes diferidos porque suman topología; el costo es que, mientras espera un reintento, el worker no toma otros mensajes (prefetch 1).
- **Duplicados.** RabbitMQ entrega "al menos una vez", así que un evento puede llegar repetido. El `id` del evento se guarda en `processed_events` en la misma transacción que el cambio de estado; un evento ya registrado se confirma sin repetir el efecto.
- **El worker es un segundo binario del mismo módulo.** Comparte dominio, casos de uso y repositorio con la API, pero es un proceso aparte para poder apagarlo y escalarlo por separado. Pedidos y notificaciones siguen siendo módulos internos, no servicios distintos.

### Trade-offs principales

| Decisión | Se ganó | Se aceptó a cambio |
|---|---|---|
| Publicar el evento después de guardar el pedido (sin outbox) | Código simple y la API no depende del broker para responder | Un pedido puede quedar sin evento si el broker falla tras los 3 intentos |
| Reintentos dentro del worker (prefetch 1) | Sin colas de espera ni plugins; política fácil de seguir y de probar | Mientras espera un reintento el worker no toma otros mensajes |
| Notificar antes de confirmar el pedido | No se pierde ninguna notificación | La notificación puede repetirse si el worker cae entre ambos pasos |
| Idempotencia opcional con tabla de claves | Resuelve reintentos y concurrencia con una restricción única, sin candados | La tabla crece (las claves no caducan) y el cliente debe enviar la clave |
| Migraciones propias, sin librería | Una dependencia menos y control del orden | Código extra que mantener (cubierto con una prueba de integración) |
| Un solo módulo Go con dos binarios (API y worker) | Dominio y repositorio compartidos, apagado independiente | No se despliegan ni versionan por separado |
| Errores RFC 9457 y códigos HTTP precisos | Estándar que cualquier cliente entiende | Respuestas de error un poco más verbosas |

## Preguntas del desafío

**¿Qué ocurre si el cliente repite el POST porque no recibió respuesta?**
Si envió la cabecera `Idempotency-Key`, el reintento no crea otro pedido: recibe `200` con el pedido original y `Idempotent-Replayed: true`, incluso si los reintentos llegan a la vez (la clave tiene restricción única en la base y se guarda en la misma transacción que el pedido). La misma clave con otro cuerpo responde `422`. Si no envió clave, se crea un pedido nuevo: la idempotencia es responsabilidad del cliente y es opcional.

**¿Qué ocurre si el worker procesa el evento y falla antes de confirmar el mensaje?**
El mensaje no se confirmó (no hubo ACK), así que RabbitMQ lo vuelve a entregar: entrega "al menos una vez". El procesamiento es idempotente: el `id` del evento se guarda en `processed_events` en la misma transacción que el cambio de estado, y un evento ya registrado se confirma sin repetir el efecto. El único efecto que puede repetirse es la notificación simulada, porque se hace antes de la transacción (se prefirió eso a perderla).

**¿Cómo se evita perder el evento si la base confirma el pedido pero el broker no está disponible?**
En este MVP no se evita del todo: el evento se publica después de confirmar la transacción, con confirmaciones del broker y 3 intentos; si igual falla, el pedido queda guardado, se registra un error con el `order_id` y el cliente recibe `201`. Ese evento se pierde y se declara como límite conocido. La solución completa es el patrón transactional outbox: guardar el evento en una tabla dentro de la misma transacción y publicarlo desde un proceso aparte, reintentando hasta lograrlo. El enunciado lo menciona como extensión; no se implementó para mantener el MVP simple, y el puerto `EventPublisher` deja el cambio localizado.

**¿Qué debe ocurrir durante un apagado mientras se procesa un mensaje?**
Al recibir SIGTERM o SIGINT la API deja de aceptar conexiones y termina las peticiones en curso; el worker deja de tomar mensajes y termina el que tiene. Todo con un plazo de 10 segundos (`SHUTDOWN_TIMEOUT`). Si el worker estaba esperando un reintento, devuelve el mensaje a la cola (sin ACK) para que no se pierda. Después se cierran la conexión a RabbitMQ y a la base, y el proceso sale con código 0; si el plazo vence, lo registra y sale con un código distinto de 0.

**¿Qué componentes pueden seguir siendo módulos del mismo servicio y por qué?**
Todo el dominio: pedidos y notificaciones son módulos internos de un solo módulo de Go, y el worker es un segundo binario (`cmd/worker`) que comparte dominio, casos de uso y repositorio con la API. Es un proceso aparte solo porque el enunciado pide un consumidor independiente y porque su apagado y su escala no deben afectar a la API. No hay equipos ni ciclos de despliegue distintos que justifiquen partirlo en microservicios, que sumarían red, despliegue y fallos sin beneficio a este tamaño.


## Limitaciones conocidas

- Si el broker no responde al crear un pedido, el pedido queda guardado pero sin evento (no se confirmará ni se notificará). Se registra el error; la mejora sería un outbox transaccional.
- Las claves de idempotencia no caducan: la tabla `idempotency_keys` crece sin límite. Una mejora sería agregar una fecha y borrar las claves viejas.
- La idempotencia es opcional: sin la cabecera, repetir el `POST` crea otro pedido.
- La notificación se hace antes de confirmar el pedido, por lo que si el worker cae justo entre ambos pasos la notificación puede repetirse (entrega "al menos una vez").
- Mientras el worker espera un reintento no procesa otros mensajes. Los mensajes de la cola de fallidos no se reprocesan solos: hay que revisarlos y reenviarlos a mano desde la consola de RabbitMQ.
- Los clientes `demo-fallo-transitorio` y `demo-fallo-permanente` existen solo para la demostración de fallos.
- No hay trazas distribuidas (OpenTelemetry), ni métricas, health checks o alertas del worker; las métricas de la API son básicas.
- `/metrics` y `/health/*` no tienen autenticación (la API tampoco); en un despliegue real se restringirían a la red interna.
- No se hizo despliegue en la nube (es opcional en el enunciado).
- No hay límite de peticiones por segundo (rate limiting).
- No hay autenticación (queda fuera del alcance del desafío).

## Posibles siguientes pasos

1. **Outbox transaccional:** guardar el evento en una tabla dentro de la misma transacción del pedido y publicarlo desde un proceso aparte, para no perder eventos si el broker falla.
2. **Reproceso de la cola de fallidos:** una herramienta o endpoint para revisar los mensajes de `orders.created.dlq` y reenviarlos a la cola principal.
3. **Reintentos sin bloquear al worker:** colas de espera con TTL (o el plugin de mensajes diferidos) y varios workers en paralelo.
4. **Idempotencia con caducidad:** agregar una fecha a `idempotency_keys` y borrar las claves viejas con una tarea periódica.
5. **Observabilidad del worker:** métricas y health propios, y trazas distribuidas con OpenTelemetry.
6. **Seguridad:** autenticación, límite de peticiones por segundo y restringir `/metrics` y `/health` a la red interna.
7. **Despliegue en la nube** (opcional en el enunciado), con la configuración por variables de entorno que ya existe.

## Cómo diagnosticar problemas

- `docker compose ps` indica si la base y RabbitMQ están `healthy`. La API espera a la base para arrancar y el worker espera a la base y a RabbitMQ (la API tolera que RabbitMQ no esté).
- `docker compose logs api` muestra los errores con su detalle real; `docker compose logs worker`, lo que hace el consumidor.
- `curl localhost:8080/health/ready` dice si la API puede hablar con la base.
- Si el puerto 8080 u 5432 están ocupados, cambiar `PORT` o `DB_PORT` en `.env`; los puertos de RabbitMQ (5672 y 15672) se cambian en `docker-compose.yml`.
- Si un pedido se queda en `created`, mirar `docker compose logs worker`: puede estar reintentando (esperas de 1, 2 y 4 segundos) o el mensaje puede haber ido a `orders.created.dlq`.

## Herramientas de apoyo

Para el desarrollo se usaron Claude Code (asistente de IA) como apoyo en distintas etapas del trabajo, además de Docker, Postman y DBeaver para levantar y comprobar el servicio. Las decisiones de alcance y de diseño están documentadas en este README, y el comportamiento se verifica con las pruebas automatizadas y la colección de Postman.
