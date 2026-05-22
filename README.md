# Source Asia – Backend Assignment

## Running the server

Requires Go 1.21 or later.

```bash
git clone <repo-url>
cd source-asia-backend
go run main.go
# → starting server on :8080
```

Override the port with the `PORT` environment variable:

```bash
PORT=9000 go run main.go
```

Run tests:

```bash
go test ./...            # all tests
go test -race ./...      # with race detector (proves concurrency safety)
go test -v ./...         # verbose output
```

---

## Architecture

### Package layout

```
source-asia-backend/
├── main.go                       entry point; reads PORT env, starts server
├── server/server.go              wires dependencies, registers routes, applies middleware
├── middleware/                   HTTP middleware: logging, body size limit, content-type check
├── internal/
│   └── httputil/respond.go      shared JSON response helpers (single source of truth)
├── ratelimit/
│   ├── store.go                  in-memory rate-limit state; all concurrency lives here
│   └── handler.go                HTTP handlers for POST /request and GET /stats
└── catalog/
    ├── store.go                  in-memory product store with split meta/media data model
    ├── handler.go                HTTP handlers for all /products endpoints
    └── validator.go              URL and field validation rules
```

### Dependency flow

```
main → server → ratelimit/handler → ratelimit/store
             → catalog/handler   → catalog/store
                                 → catalog/validator
             → middleware
             → internal/httputil  (shared by all handlers)
```

No package imports its own parent. `internal/httputil` is the only shared code and is not importable outside this module.

### Why no framework

The standard library `net/http` ServeMux (Go 1.22+) supports method-qualified patterns like `"POST /request"` and path wildcards like `"GET /products/{id}"` natively. Adding a router framework would be dead weight for this surface area.

---

## Design Principles

### SOLID

**Single Responsibility**
Each package has one job. `ratelimit/store.go` manages window state — it knows nothing about HTTP. `ratelimit/handler.go` handles HTTP — it knows nothing about how the window works. `catalog/validator.go` validates fields — it has no awareness of HTTP or storage.

**Open/Closed**
New middleware (auth, CORS, metrics) can be added to the `middleware.Chain(...)` call in `server.go` without touching any handler. New endpoints are registered in the same place without touching existing routes. Neither the store nor the handler needs to change when the other evolves.

**Liskov Substitution**
`server.New()` returns `http.Handler`, not a concrete `*Server`. Any handler chain satisfying `http.Handler` is substitutable — the caller never depends on the concrete type.

**Interface Segregation**
No large interfaces are defined. `http.Handler` and `http.ResponseWriter` are the only interfaces in use — both defined by the standard library and both minimal. The stores are used directly as concrete types within their packages; no interface is introduced until there is a real reason (e.g. testing with a mock).

**Dependency Inversion**
`ratelimit.NewHandler` and `catalog.NewHandler` receive their stores as constructor arguments rather than constructing them internally. This makes the dependency explicit, testable, and replaceable without modifying handler code. Tests pass in a fresh store; production passes in the singleton store.

### 12-Factor App

| Factor | How it applies |
|---|---|
| **I — Codebase** | One repo, one service binary. All configuration comes from the environment. |
| **II — Dependencies** | `go.mod` declares the module. No external dependencies — standard library only. All dependencies are explicit and reproducible. |
| **III — Config** | `PORT` is the only config value; it is read from the environment, not hardcoded, and has a documented default. Adding more config (timeouts, limits) follows the same pattern. |
| **IV — Backing services** | No backing services in this version (in-memory). Attaching one (Redis for rate-limiting, Postgres for products) would mean injecting a client into the store — the handler code does not change. |
| **V — Build / release / run** | `go build` produces a single self-contained binary. The binary reads environment at startup — no separate config files. |
| **VI — Processes** | The process is stateless per request. Shared mutable state (stores) is confined to the process and explicitly documented as a production limitation. |
| **VII — Port binding** | The server binds to a port and exports HTTP directly. The port is injected via environment (`PORT`). |
| **VIII — Concurrency** | The service can handle concurrent requests safely (see concurrency section below). Scaling out would be done by running multiple instances behind a load balancer with a shared backing store. |
| **IX — Disposability** | Startup is fast (no external connections). Shutdown is clean — `http.ListenAndServe` returns on signal and the process exits with a logged error. |
| **X — Dev/prod parity** | No dev-only code paths. The same binary runs in both environments. In-memory storage is a documented limitation, not a dev shortcut. |
| **XI — Logs** | All logs go to stdout via `log.Printf`. The request logger emits one line per request: method, path, status, duration. No log files, no log rotation — the process does not manage its own output. |
| **XII — Admin processes** | No admin processes. The optional seed loop in the README is a one-off `curl` script run against the live API — not a special code path. |

### Concurrency model

**Part 1 — Rate limiter**

Two-level locking:

- `Store.mu` (`sync.RWMutex`) guards the `users` map. All concurrent reads (lookups of existing users) acquire a read lock and never block each other. A write lock is only taken when a new user entry is created for the first time.
- `userState.mu` (`sync.RWMutex`) guards the per-user counters. `tryAccept` takes an exclusive write lock (it mutates `acceptedCount`). `snapshot` takes a read lock (it only reads).

This means concurrent requests for different users never contend at all, and concurrent reads from the stats endpoint do not block ongoing requests.

The accept/reject decision and the counter increment happen atomically inside the same write lock — there is no window where two goroutines both see "count < max" before either increments.

**Part 2 — Product catalog**

A single `sync.RWMutex` guards all four internal maps. Reads (`GetProduct`, `ListProducts`) hold a shared read lock. Writes (`CreateProduct`, `AddMedia`) hold an exclusive write lock. Entries are never deleted, so there is no risk of accessing a deleted pointer after releasing the lock.

All data returned by store methods is deep-copied. Callers cannot mutate internal state through the returned slices, and the store is unaffected by mutations to the input slices passed in.

### Data model (Part 2)

Products are stored in two separate maps:

```
metas  map[id]*ProductMeta    id, name, sku, image_count, video_count, created_at
media  map[id]*ProductMedia   image_urls[], video_urls[]
skuIdx map[sku]string         for O(1) duplicate detection on create
order  []string               insertion-ordered IDs for stable pagination
```

`GET /products` (list) reads only `metas`. It never touches `media`. With 1,000 products each having 10 image URLs stored, the list endpoint touches zero URL strings — it reads 1,000 small structs and returns `limit` of them.

`GET /products/{id}` (detail) is the only place that loads `media`.

| Endpoint | Maps accessed | URL arrays loaded |
|---|---|---|
| `GET /products?limit=20` | `metas` only | never |
| `GET /products/{id}` | `metas` + `media` | yes, all |
| `POST /products` | all (write) | yes, stored |
| `POST /products/{id}/media` | `metas` + `media` (write) | appended |

---

## Code Style

**Naming**
Follows standard Go conventions: `MixedCaps` for exported names, `camelCase` for unexported. Acronyms are cased consistently (`userID` not `userId`, `SKU` not `Sku`). Test functions use the `Test<Subject>_<Condition>` pattern for scannable output.

**Errors**
Sentinel errors are `var` declarations using `errors.New`, not string types. This makes them comparable with `errors.Is` and wrappable with `fmt.Errorf("%w", ...)`. Error messages do not include full URLs to avoid echoing credentials that may appear in a URL (`user:pass@host`). The index is included in URL slice errors (`image_urls[1]: ...`) so the caller knows exactly which entry failed.

**No panic in handlers**
Every error path returns explicitly. No handler uses `panic` or relies on a recover middleware.

**Middleware is composable**
`middleware.Chain(handler, mw1, mw2, mw3)` applies middleware in declared order (left to right). Each middleware is a plain `func(http.Handler) http.Handler` — no framework type, no interface to implement.

**No unnecessary abstraction**
Stores are concrete types. No `StoreInterface` is defined because nothing substitutes for it in this codebase. The handler tests call handler methods directly and inject a real store — no mocks.

**Shared code lives in `internal/`**
`internal/httputil` is the only shared package. The `internal/` path means it cannot be imported from outside this module, which is appropriate for helpers that are an implementation detail.

---

## Part 1 – Rate Limiting

### Window type

Fixed 1-minute window per user. The window starts on the first request and resets 60 seconds later. Rolling windows give smoother limiting at window boundaries but need a timestamp ring buffer per user and are harder to get right under concurrency.

Limit: **5 accepted requests per user per window.**

### Rejected counter

`rejected_total` is cumulative — it never resets. This makes it useful as a monitoring signal for total overload events, not just the current window.

---

### POST /request

Returns `201 Created` on success. 201 is chosen over 200 because accepting the request creates a record in the rate-limit window.

**Request:**
```json
{ "user_id": "alice", "payload": { "any": "value" } }
```

Both fields are required. `user_id` must be non-empty (whitespace-only is rejected). `payload` can be any valid non-null JSON value — including `false`, `0`, and `""`.

**201 – accepted:**
```json
{
  "status": "accepted",
  "user_id": "alice",
  "payload": { "any": "value" },
  "message": "request accepted"
}
```

**429 – rate limited:**
```json
{ "error": "rate limit exceeded: maximum 5 requests per minute per user" }
```

**400 – bad input:**
```json
{ "error": "user_id is required and must be non-empty" }
```

---

### GET /stats

```json
{
  "users": {
    "alice": {
      "accepted_in_current_window": 3,
      "rejected_total": 7
    }
  }
}
```

`accepted_in_current_window` returns 0 if the window has expired. `rejected_total` is cumulative.

---

### curl examples (Part 1)

```bash
# accept a request
curl -s -X POST http://localhost:8080/request \
  -H "Content-Type: application/json" \
  -d '{"user_id":"alice","payload":{"item":"book"}}'

# trigger the rate limit
for i in $(seq 1 6); do
  curl -s -X POST http://localhost:8080/request \
    -H "Content-Type: application/json" \
    -d '{"user_id":"alice","payload":{"i":'$i'}}' | jq .
done

# check stats
curl -s http://localhost:8080/stats | jq .
```

---

### Production limitations

**Single instance only.** All state is in one process. Two instances behind a load balancer would each maintain independent counters, allowing up to `5 × N` requests per window. Fix: move rate-limit state into Redis using `INCR` + key expiry, which is atomic across instances.

**Restart wipes state.** All counters reset on restart or crash.

**Memory grows unboundedly.** A `userState` is created for every distinct `user_id` seen and is never evicted. At high user-ID cardinality this leaks memory. Fix: add TTL-based eviction, or use Redis where key expiry is built in.

---

## Part 2 – Product Catalog

### Validation rules

| Rule | Detail |
|---|---|
| `name`, `sku` | Required, non-empty after trimming whitespace |
| URL scheme | Must be `http://` or `https://` |
| URL max length | 2048 characters |
| URLs per request | Maximum 20 per array (`image_urls` or `video_urls`) |
| Duplicate `sku` | Returns `409 Conflict` — the request is syntactically valid, the conflict is a business constraint |

URLs are never echoed back in error messages. If a URL contains credentials (`user:pass@host`), they are not leaked in the response.

---

### POST /products

**Request:**
```json
{
  "name": "Widget A",
  "sku": "SKU-001",
  "image_urls": [
    "https://cdn.example.com/products/sku-001/img-1.jpg",
    "https://cdn.example.com/products/sku-001/img-2.jpg"
  ],
  "video_urls": [
    "https://cdn.example.com/products/sku-001/demo.mp4"
  ]
}
```

`image_urls` and `video_urls` are optional and default to empty arrays.

**201 – created:**
```json
{
  "id": "prod_000001",
  "name": "Widget A",
  "sku": "SKU-001",
  "image_count": 2,
  "video_count": 1,
  "created_at": "2024-01-15T10:00:00Z",
  "image_urls": ["https://cdn.example.com/products/sku-001/img-1.jpg", "..."],
  "video_urls": ["https://cdn.example.com/products/sku-001/demo.mp4"]
}
```

**409 – duplicate SKU:**
```json
{ "error": "a product with this SKU already exists" }
```

---

### GET /products

List endpoint. Never includes `image_urls` or `video_urls` — only counts.

Query params: `limit` (default 20, max 100 — values above max are clamped, not rejected), `offset` (default 0).

**200:**
```json
{
  "products": [
    {
      "id": "prod_000001",
      "name": "Widget A",
      "sku": "SKU-001",
      "image_count": 2,
      "video_count": 1,
      "created_at": "2024-01-15T10:00:00Z"
    }
  ],
  "total": 1,
  "offset": 0,
  "limit": 20
}
```

---

### GET /products/{id}

Returns the full product including all URL arrays.

**200:**
```json
{
  "id": "prod_000001",
  "name": "Widget A",
  "sku": "SKU-001",
  "image_count": 2,
  "video_count": 1,
  "created_at": "2024-01-15T10:00:00Z",
  "image_urls": ["https://cdn.example.com/products/sku-001/img-1.jpg"],
  "video_urls": ["https://cdn.example.com/products/sku-001/demo.mp4"]
}
```

**404:** `{ "error": "product not found" }`

---

### POST /products/{id}/media

Appends URLs. At least one non-empty array is required.

**Request:**
```json
{ "image_urls": ["https://cdn.example.com/products/sku-001/img-3.jpg"] }
```

**200:** Full product with updated counts and all URLs.

---

### curl examples (Part 2)

```bash
# create a product
curl -s -X POST http://localhost:8080/products \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Widget A",
    "sku": "SKU-001",
    "image_urls": [
      "https://cdn.example.com/products/sku-001/img-1.jpg",
      "https://cdn.example.com/products/sku-001/img-2.jpg"
    ],
    "video_urls": ["https://cdn.example.com/products/sku-001/demo.mp4"]
  }' | jq .

# list (no URLs in response)
curl -s "http://localhost:8080/products?limit=10&offset=0" | jq .

# detail (full URLs)
curl -s http://localhost:8080/products/prod_000001 | jq .

# add media
curl -s -X POST http://localhost:8080/products/prod_000001/media \
  -H "Content-Type: application/json" \
  -d '{"image_urls":["https://cdn.example.com/products/sku-001/img-3.jpg"]}' | jq .

# duplicate SKU → 409
curl -s -X POST http://localhost:8080/products \
  -H "Content-Type: application/json" \
  -d '{"name":"Dupe","sku":"SKU-001"}' | jq .

# seed 20 products
for i in $(seq 1 20); do
  curl -s -X POST http://localhost:8080/products \
    -H "Content-Type: application/json" \
    -d '{
      "name": "Product '"$i"'",
      "sku": "SKU-'"$(printf '%03d' $i)"'",
      "image_urls": [
        "https://cdn.example.com/p'"$i"'/img-1.jpg",
        "https://cdn.example.com/p'"$i"'/img-2.jpg"
      ],
      "video_urls": ["https://cdn.example.com/p'"$i"'/demo.mp4"]
    }' > /dev/null
done
curl -s "http://localhost:8080/products?limit=5" | jq .
```

---

### What would change with PostgreSQL + CDN

```sql
-- List queries only touch this table — no join, no media load
CREATE TABLE products (
  id          BIGSERIAL PRIMARY KEY,
  name        TEXT NOT NULL,
  sku         TEXT NOT NULL UNIQUE,
  image_count INT NOT NULL DEFAULT 0,
  video_count INT NOT NULL DEFAULT 0,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Only loaded on detail queries, indexed by product_id
CREATE TABLE product_media (
  id          BIGSERIAL PRIMARY KEY,
  product_id  BIGINT NOT NULL REFERENCES products(id) ON DELETE CASCADE,
  media_type  TEXT NOT NULL CHECK (media_type IN ('image', 'video')),
  url         TEXT NOT NULL,
  position    INT NOT NULL DEFAULT 0
);
CREATE INDEX ON product_media(product_id);
```

`image_count` and `video_count` stay as denormalised columns on `products` to avoid a `COUNT(*)` on every list query. They are updated by the application on every `AddMedia` call (or via a database trigger).

For the CDN: store only the path segment (`/products/sku-001/img-1.jpg`) in the database and prepend the CDN base URL from environment config at read time. Switching CDN providers requires no data migration.
