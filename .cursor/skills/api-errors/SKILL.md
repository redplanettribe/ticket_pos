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

**Path ids are the exception to "bad UUID is `VALIDATION_FAILED`".**
A malformed id in the PATH names a resource that cannot exist, so it is answered as a well-formed id naming nothing: `404` with the resource's own code (`EVENT_NOT_FOUND`, ...), or the empty list on a read whose "not found" is an empty list.
It never reaches a uuid column (Postgres would refuse it and the request would 500).
It is refused after the route's gate admits the caller and before the body is read.
Staff and operator routes get this from one guard, `internal/server/path_ids.go`: a new id wildcard there goes in its table, with the error its unknown id answers (method-specific where one verb answers about something else, as `DELETE answers/{questionId}` answers `ANSWER_NOT_FOUND`).
The guard fails closed: a staff or operator route with a wildcard that is neither in the table nor in the commented non-id exemptions panics at `RegisterRoutes`, so the server does not start.
`TestEveryRouteRefusesAMalformedPathIDAsNotFound` walks every registered route, fails on a 5xx, and on a read or a delete whose other ids are real requires a malformed id to get exactly the status and code a well-formed unknown one gets.
A route whose "not found" is an empty list is left out of the guard's table and gives a malformed id that same empty list where its unknown id gets one.
For the staff Ticket Sale answers read that is the service, after the Event is resolved, on the same ordering argument as `INVALID_HOLDER_EMAIL` above.
The customer and Assignment Link answer writes have no guard, and their question id is checked the same way: the shared answer step refuses a malformed one as `TICKET_QUESTION_NOT_FOUND` after the Ticket is resolved, so the Ticket's own not-found still comes first.
Ids in the query string or body stay ordinary validation: `400 VALIDATION_FAILED`, field code `INVALID_ID`.

Validation `details` shape:

```json
{
  "fields": [
    { "field": "price", "message": "must be greater than 0" }
  ]
}
```

#### Named exception: `INVALID_HOLDER_EMAIL` (id-oracle ordering)

The Holder email on the buyer's assign-ticket write
(`catalog.ErrInvalidHolderEmail`, `internal/catalog/service/ticket_assignment.go`)
stays a **service-layer domain error**, deliberately.

The rule it protects: a Ticket that is not the caller's answers 404
`TICKET_NOT_FOUND`, indistinguishable from one that never existed. If the
handler parsed the address, a malformed address on somebody else's Ticket
would return 400 before the Ticket was resolved — telling the caller their
guessed id was at least reachable. The service resolves the Ticket **first**
and only then parses the address, so the ordering is part of the disclosure
rule (ADR 0035), not a validation convenience.

The exception is only for fields whose early rejection would leak whether an
id or resource exists. It does not extend to sibling fields with no id to
leak: the Holder *name* on the Assignment Link write is ordinary handler
validation (`VALIDATION_FAILED`), because the signed token names the Ticket
(#336).

### Domain errors

Services return typed errors with stable codes.
Each domain module owns its types (e.g. `catalog.ErrEventNotFound`, `sales.ErrCapacityExceeded`).

`platform/httputil` maps domain errors to HTTP status:

| Pattern | HTTP | Example `code` |
|---------|------|----------------|
| Not found in org | 404 | `EVENT_NOT_FOUND` |
| Conflict / state violation | 409 | `CAPACITY_EXCEEDED` |
| Permission (domain) | 403 | `FORBIDDEN` |

A refusal about capacity, where the same request a moment later is expected to succeed, also declares a `Retry-After` in `domainRetryAfter` beside the status table (today only `HOLDER_EXPORT_BUSY`).
The declaration decides the request log level too: a 5xx is logged at ERROR, mapped or not, unless it carries a `Retry-After`, which makes it a designed refusal logged at WARN.
A 4xx is logged at INFO.

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
