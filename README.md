# Image Thumbnail API

Go REST service that accepts image uploads, generates aspect-ratio-preserving thumbnails (presets or custom sizes), stores metadata in MongoDB, and serves generated files from local disk.

> **Status:** Project scaffold and configuration only. Business logic (upload, resize, persistence) is not implemented yet.

## Architecture

```text
Handler → Service → Repository → MongoDB
              └→ Image Processor → Local File Storage
```

See [docs/architecture.md](docs/architecture.md) for the request lifecycle and data-flow diagram. Design rationale lives in [DECISIONS.md](DECISIONS.md).

## Planned API

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/images` | Upload one or more images and generate thumbnails |
| `GET` | `/v1/thumbnails/{id}` | Thumbnail metadata |
| `GET` | `/v1/thumbnails/{id}/file` | Thumbnail binary |
| `GET` | `/healthz` | Liveness |
| `GET` | `/readyz` | Readiness (MongoDB ping) |

## Project layout

```text
cmd/server/          Application entrypoint
internal/
  config/            Environment-based configuration
  handler/           HTTP handlers
  service/           Use-case orchestration
  repository/        MongoDB metadata access
  model/             Domain structs
  image/             Resize helpers / presets
data/
  originals/         Stored original images
  thumbnails/        Stored generated thumbnails
docs/                Architecture notes
```

## Prerequisites

- Go 1.22+ (module targets the toolchain in `go.mod`)
- Docker and Docker Compose (recommended)
- MongoDB 7 (provided by Compose)

## Configuration

Copy the example env file and adjust as needed:

```bash
cp .env.example .env
```

| Variable | Default | Meaning |
|----------|---------|---------|
| `HTTP_PORT` | `8080` | HTTP listen port |
| `MONGO_URI` | `mongodb://localhost:27017` | MongoDB connection string |
| `MONGO_DATABASE` | `thumbnails` | Database name |
| `DATA_DIR` | `./data` | Root for `originals/` and `thumbnails/` |
| `MAX_UPLOAD_BYTES` | `10485760` | Max bytes per uploaded file (10 MiB) |
| `MAX_FILES_PER_REQUEST` | `5` | Max files in one upload |
| `MAX_IMAGE_PIXELS` | `25000000` | Max decoded width×height |
| `MAX_CUSTOM_DIMENSION` | `2000` | Max custom width or height |

## Run with Docker Compose

```bash
docker compose up --build
```

- API: `http://localhost:8080`
- MongoDB: `localhost:27017`

Health checks:

```bash
curl -s http://localhost:8080/healthz
curl -s http://localhost:8080/readyz
```

## Run locally (without Compose for the API)

1. Start MongoDB (Compose Mongo only, or any local instance):

   ```bash
   docker compose up mongo -d
   ```

2. Export env vars (or rely on defaults with `MONGO_URI=mongodb://localhost:27017`).

3. Run the server:

   ```bash
   go run ./cmd/server
   ```

## Develop

```bash
go test ./...
go vet ./...
go build -o bin/server ./cmd/server
```

## CI

GitHub Actions (`.github/workflows/ci.yml`) runs `go vet`, `go test -race`, binary build, and `docker build` on pushes and pull requests.

## Next steps

Implement business logic in this order:

1. Validation + resize math (`internal/image`)
2. Repository CRUD (`internal/repository`)
3. Service orchestration + filesystem writes (`internal/service`)
4. Handler wiring + error mapping (`internal/handler`)
5. Unit and integration tests
