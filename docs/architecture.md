# Architecture

## Overview

Production-oriented thumbnail service with a clear layered design:

- **HTTP handlers** accept requests and map them to service calls
- **Service** owns use-case orchestration (validate → resize → store → persist)
- **Repository** stores image/thumbnail **metadata** in MongoDB
- **Image processor** resizes images (aspect ratio preserved)
- **Local filesystem** stores original and thumbnail **bytes**

```text
Client
  │
  │  POST /v1/images (multipart)
  │  GET  /v1/thumbnails/{id}
  │  GET  /v1/thumbnails/{id}/file
  ▼
┌─────────────────────────────────────────┐
│  Handler (internal/handler)             │
│  - parse HTTP / multipart               │
│  - map errors → status codes            │
└─────────────────┬───────────────────────┘
                  │
                  ▼
┌─────────────────────────────────────────┐
│  Service (internal/service)             │
│  - input validation                     │
│  - coordinate resize + IO               │
│  - bounded concurrency for work items   │
└───────┬─────────────────────┬───────────┘
        │                     │
        ▼                     ▼
┌───────────────────┐   ┌─────────────────────────┐
│ Image Processor   │   │ Repository              │
│ (internal/image)  │   │ (internal/repository)   │
│ - fit-inside box  │   │ - MongoDB metadata      │
│ - presets/custom  │   └───────────┬─────────────┘
└─────────┬─────────┘               │
          │                         ▼
          │               ┌─────────────────────┐
          │               │ MongoDB             │
          │               │ images / thumbnails │
          ▼               └─────────────────────┘
┌─────────────────────────┐
│ Local File Storage      │
│ data/originals/         │
│ data/thumbnails/        │
└─────────────────────────┘
```

## Request lifecycle (upload)

1. Client sends one or more image files via `multipart/form-data`.
2. Handler receives the request and delegates to the service.
3. Service validates count, size, type, and resize options.
4. For each file:
   - decode and inspect dimensions
   - write original bytes under `data/originals/`
   - generate thumbnails (presets and/or custom box)
   - write thumbnail bytes under `data/thumbnails/`
   - insert image + thumbnail documents in MongoDB
5. Handler returns `201` with metadata and retrieval URLs.

## Request lifecycle (retrieve)

1. Client requests thumbnail metadata or file by ID.
2. Service loads metadata from MongoDB.
3. For file download, service resolves the local path and streams bytes.
4. Missing documents or files return `404`.

## Data flow

| Data | Store | Why |
|------|--------|-----|
| Original image bytes | Local filesystem | Simple, fast local IO for this exercise |
| Thumbnail bytes | Local filesystem | Same as originals; easy to serve |
| Image / thumbnail metadata | MongoDB | Query by ID, stable API responses without scanning disk |

## Concurrency

- Each HTTP request runs in its own goroutine (standard `net/http`).
- Within a request, resize work should use a **bounded** worker pool so large batches cannot exhaust memory/CPU.
- MongoDB driver and unique file IDs avoid shared mutable state.

## Health endpoints

| Path | Purpose |
|------|---------|
| `GET /healthz` | Process is up |
| `GET /readyz` | MongoDB is reachable |

## Out of scope (intentionally)

Async job queues, S3/object storage, auth, CDN, and multi-region replication are deferred so the exercise stays finishable. See `DECISIONS.md`.
