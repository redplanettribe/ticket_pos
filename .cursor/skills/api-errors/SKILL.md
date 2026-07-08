---
name: api-errors
description: >-
  Ticket POS API response envelope and layered error handling. Use whenever
  adding or changing HTTP handlers, service errors, OpenAPI error schemas,
  platform/httputil helpers, or reviewing API responses. Trigger on phrases
  like "API error", "response envelope", "domain error", "handler validation",
  "error code", "standardized response", or when implementing endpoints in
  backend/ even if the user does not mention errors explicitly.
---

# API Response and Error Handling

Ticket POS uses one JSON envelope for every API response.
Errors are separated by **layer** (handler vs domain), not by response shape.

The canonical spec lives in [docs/technical-design.md](../../../docs/technical-design.md) under **API design → Standard response envelope** and **Errors**.
Read that section before implementing or reviewing API changes.

## Response envelope

Every JSON response:

```json
{
  "data": <resource or null>,
  "error": <error object or null>,
  "request_id": "<uuid>"
}
```

| Field | Success | Failure |
|-------|---------|---------|
| `data` | Payload directly (no wrapper key like `event`) | `null` |
| `error` | `null` | Error object |
| `request_id` | Always present | Always present |

`request_id` mirrors `X-Request-ID` on every response.

## Error object

Same shape for handler and domain errors.
Clients distinguish by `code`, not structure.

```json
{
  "code": "CAPACITY_EXCEEDED",
  "message": "Not enough tickets remaining for VIP",
  "details": {}
}
```

| Field | Required | Notes |
|-------|----------|-------|
| `code` | Yes | Stable machine-readable contract |
| `message` | Yes | Safe to show in Storefront and Staff UI |
| `details` | No | Structured context; shape varies by `code` |

Do **not** add a `kind` discriminator.
Separation is architectural, not structural.

## Layer responsibilities

```
Request → Handler → Service → Repository
              ↑         ↑
         HTTP/validation  Domain errors
         Handler errors   (no HTTP knowledge)
```

| Layer | Owns | Never does |
|-------|------|------------|
| **Handler** | JSON parsing, auth, input validation, HTTP status, envelope | Business rules |
| **Service** | Business rules, typed domain errors | HTTP status, JSON shape |
| **Repository** | SQL | Business logic, errors with HTTP meaning |
| **platform/httputil** | Envelope types, `WriteEnvelope`, domain-error → HTTP mapper | Domain-specific error definitions |

### Handler errors

Reject before calling the service.

| Situation | HTTP | `code` |
|-----------|------|--------|
| Malformed JSON | 400 | `INVALID_JSON` |
| Validation failure | 400 | `VALIDATION_FAILED` |
| Missing/invalid auth | 401 | `UNAUTHORIZED` |
| Permission denied | 403 | `FORBIDDEN` |
| Route/resource not found | 404 | `NOT_FOUND` |
| Wrong HTTP method | 405 | `METHOD_NOT_ALLOWED` |
| Unhandled panic/recoverable failure | 500 | `INTERNAL_ERROR` |

**Validation is always handler responsibility.**
Shape, required fields, types, and formats (`price: -5`, bad UUID) never reach the service.

Validation `details` shape:

```json
{
  "fields": [
    { "field": "price", "message": "must be greater than 0" }
  ]
}
```

### Domain errors

Services return typed errors with stable codes.
Each domain module owns its types (e.g. `catalog.ErrEventNotFound`, `sales.ErrCapacityExceeded`).

`platform/httputil` maps domain errors to HTTP status:

| Pattern | HTTP | Example `code` |
|---------|------|----------------|
| Not found in org | 404 | `EVENT_NOT_FOUND` |
| Conflict / state violation | 409 | `CAPACITY_EXCEEDED` |
| Permission (domain) | 403 | `FORBIDDEN` |

Handlers stay thin:

1. Validate request (handler).
2. Call service.
3. On error, pass to `httputil.WriteError` (or equivalent mapper).
4. On success, `httputil.WriteSuccess`.

Services that need DB context for rules (duplicate ticket type name, capacity below sold count) return domain errors - not validation errors.

### Sale Import batch failures

All-or-nothing batches return the **first** failing row only:

```json
{
  "code": "IMPORT_BATCH_FAILED",
  "message": "Import would oversell at row 200",
  "details": { "row": 200, "reason": "CAPACITY_EXCEEDED" }
}
```

## Go implementation patterns

### platform/httputil types

```go
type Envelope struct {
    Data      any        `json:"data"`
    Error     *APIError  `json:"error"`
    RequestID string     `json:"request_id"`
}

type APIError struct {
    Code    string `json:"code"`
    Message string `json:"message"`
    Details any    `json:"details,omitempty"`
}
```

### Domain error interface

Each module defines sentinel errors implementing a small interface:

```go
// platform/apperror or platform/httputil
type DomainError interface {
    error
    Code() string
    Message() string
    Details() any
}
```

Module example:

```go
// sales/errors.go
var ErrCapacityExceeded = &capacityExceededError{...}

type capacityExceededError struct {
    TicketTypeID string
    Requested    int
    Remaining    int
}

func (e *capacityExceededError) Code() string    { return "CAPACITY_EXCEEDED" }
func (e *capacityExceededError) Message() string { return "..." }
func (e *capacityExceededError) Details() any { return map[string]any{...} }
```

### Handler sketch

```go
func (h *Handler) CreateTicketType(w http.ResponseWriter, r *http.Request) {
    reqID := platform.RequestID(r.Context())

    var req createTicketTypeRequest
    if err := decodeAndValidate(r, &req); err != nil {
        platform.WriteValidationError(w, reqID, err)
        return
    }

    result, err := h.svc.CreateTicketType(r.Context(), orgID, req.toInput())
    if err != nil {
        platform.WriteDomainError(w, reqID, err)
        return
    }

    platform.WriteSuccess(w, reqID, result)
}
```

### What not to do

- Do not return raw `http.Error` text bodies for API routes (except maybe pre-middleware failures).
- Do not put HTTP status codes in service-layer code.
- Do not wrap success payloads under a type-specific key in `data`.
- Do not mix handler validation with service validation for the same field constraints.
- Do not return all failing import rows at launch.

## OpenAPI

Define reusable schemas in `openapi/openapi.yaml`:

- `Envelope` (or `APIResponse`) with `data`, `error`, `request_id`
- `APIError` with `code`, `message`, `details`
- Endpoint responses reference the envelope; `data` uses endpoint-specific schemas

After handler changes, run `make swagger` (see [update-swagger](../update-swagger/SKILL.md)).

## Checklist for new endpoints

- [ ] Success response uses envelope with bare resource in `data`
- [ ] `error` is `null` on success
- [ ] `request_id` in body and `X-Request-ID` header
- [ ] Handler validates input before service call
- [ ] Domain errors defined in owning module with stable `code`
- [ ] Handler maps service errors via platform mapper (not ad-hoc per handler)
- [ ] OpenAPI documents error responses with shared `APIError` schema
- [ ] `message` is customer/staff safe (no stack traces, no internal IDs unless useful)

## When updating the design

If a grilling session or ADR changes error conventions, update `docs/technical-design.md` first, then align this skill.
