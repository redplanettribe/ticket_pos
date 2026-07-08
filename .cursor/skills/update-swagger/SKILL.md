---
name: update-swagger
description: Regenerate Swagger/OpenAPI docs after backend API changes. Use when adding, changing, or removing HTTP handlers, routes, request/response types, or swag annotations in the Go backend.
---

# Update Swagger

After any API change in `backend/`, run:

```bash
make swagger
```

This regenerates `backend/docs/` from swag annotations and copies the spec to `openapi/openapi.yaml`. Commit the updated generated files with the API change.

## New endpoints

Add swag comments on the handler (see `backend/cmd/server/main.go` for the pattern):

```go
// @Summary      ...
// @Tags         ...
// @Produce      json
// @Success      200  {object}  responseType
// @Router       /path [get]
```

Then run `make swagger`.

## Verify

Swagger UI: `http://localhost:8080/swagger/index.html` (with backend running).
