FROM golang:1.25-alpine AS builder
WORKDIR /build

# Copy workspace and both modules so go.work is resolvable during build.
COPY go.work go.work.sum ./
COPY services/internal/ ./services/internal/
COPY services/digest/ ./services/digest/

# Build with version injection. Context must be the repo root.
ARG VERSION=dev
RUN go build \
    -ldflags "-X main.version=${VERSION}" \
    -o /bin/digest \
    ./services/digest/cmd/digest/

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata

# Migrations are read at runtime from this path relative to WORKDIR.
WORKDIR /app
COPY --from=builder /bin/digest /bin/digest
COPY services/digest/migrations/ ./services/digest/migrations/

EXPOSE 8080
ENTRYPOINT ["/bin/digest"]
