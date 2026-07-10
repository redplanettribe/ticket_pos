---
name: update-swagger
description: Regenerate Swagger/OpenAPI docs after backend API changes. Use when adding, changing, or removing HTTP handlers, routes, request/response types, or swag annotations in the Go backend.
---

# Update Swagger

After any API change in `backend/`, run:

```bash
make openapi
```

This regenerates `backend/docs/` from swag annotations, copies the spec to `openapi/openapi.yaml`, and regenerates `packages/api-client`.

To refresh only the spec:

```bash
make swagger
```

## New endpoints

Add swag comments on the handler (see `backend/cmd/server/main.go` and `backend/internal/identity/handler/handler.go` for patterns):

```go
// @Summary      ...
// @Tags         staff
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body      requestBody  true  "..."
// @Success      200   {object}  openapi.EnvelopeSession
// @Failure      400   {object}  platform.Envelope
// @Failure      401   {object}  platform.Envelope
// @Router       /api/v1/staff/... [get]
```

Use typed envelope wrappers from `backend/internal/identity/openapi/` for `@Success` responses so `packages/api-client` gets real `data` types.

Use `platform.Envelope` for `@Failure` responses.

## Verify

Swagger UI: `http://localhost:8080/swagger/index.html` (with backend running).

Contract test: `go test ./internal/identity/openapi/...` from `backend/`.

CI runs `make openapi-sync-check` to ensure spec, generated docs, and api-client stay in sync.
