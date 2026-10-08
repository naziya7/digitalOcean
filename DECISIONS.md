# Design decisions

This document records important choices for the image thumbnail API and **why** they were made.

## Architecture: Handler → Service → Repository (+ Image Processor / FS)

**Decision:** Keep a small layered monolith.

**Why:** Clear separation of HTTP, business rules, and persistence without introducing microservices. Easy to test and explain in a short exercise.

## Metadata in MongoDB, bytes on local disk

**Decision:** MongoDB stores image/thumbnail metadata; the filesystem stores binary files.

**Why:**

- Metadata needs stable IDs and lookups — a database is a better fit than scanning directories.
- Image bytes are large and opaque — the filesystem (or later object storage) is the right place.
- Splitting concerns makes a future move to S3 straightforward without rewriting the API.

## Sync processing (not a job queue)

**Decision:** Upload requests resize images inline and return metadata in the same response.

**Why:** A queue + workers + polling is correct for large production workloads but is over-engineering for a ~3-hour exercise. Sync keeps the demo path short and testable.

## Aspect-ratio behavior: fit inside box, no upscale

**Decision:** Presets/custom sizes define a bounding box. Output fits inside that box, preserves aspect ratio, and does not enlarge smaller images.

**Why:** Matches the functional requirement, avoids distorted or blurry results, and is simple to reason about.

## Preset sizes

| Preset | Box |
|--------|-----|
| small | 150×150 |
| medium | 300×300 |
| large | 600×600 |

**Why:** Square boxes work for both portrait and landscape under “fit inside,” and the numbers are easy to demo.

## Standard library HTTP (`net/http`)

**Decision:** No Gin/Chi/Echo for the initial scaffold.

**Why:** Go 1.22+ routing is enough for a handful of endpoints; fewer dependencies means less setup cost.

## MongoDB Go Driver v2

**Decision:** Use `go.mongodb.org/mongo-driver/v2`.

**Why:** Current supported driver line; v1 is deprecated.

## Configuration via environment variables

**Decision:** Load settings from the environment (see `.env.example`). No third-party config library in the scaffold.

**Why:** Works the same locally, in Docker Compose, and in CI. Keeps the dependency footprint small.

## Docker Compose includes MongoDB + API

**Decision:** One compose file runs the API and MongoDB with a named volume for image data.

**Why:** Reviewers can start the stack with a single command; data survives container restarts.

## What we intentionally skipped (for now)

- Object storage (S3/MinIO)
- Authentication / multi-tenancy
- Async resize jobs and webhooks
- Prometheus / OpenTelemetry
- Soft deletes and retention policies

**Why:** Each is valuable in production, but implementing them now would risk an unfinished exercise. They are natural follow-ups once the core path works.
