FROM golang:1.26.5-alpine AS builder

WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY . .

# Both binaries ship in one image: the API service and the migrate Job deploy
# from the same digest, differing only in entrypoint. Migrations are go:embed'ed
# into the migrate binary, so no SQL files need copying.
RUN CGO_ENABLED=0 GOOS=linux go build -o /server ./cmd/server \
  && CGO_ENABLED=0 GOOS=linux go build -o /migrate ./cmd/migrate

FROM alpine:3.20

RUN apk add --no-cache ca-certificates

# The commit SHA this image was built from, which the error reporter sends as
# the release (ADR 0079). The deploy workflow passes it; a build without it
# still works, and its reports carry no release.
ARG SENTRY_RELEASE=""
ENV SENTRY_RELEASE=${SENTRY_RELEASE}

COPY --from=builder /server /server
COPY --from=builder /migrate /migrate

EXPOSE 8080

ENTRYPOINT ["/server"]
