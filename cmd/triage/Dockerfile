FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /triage ./cmd/triage

FROM gcr.io/distroless/static-debian12
COPY --from=builder /triage /triage
# gh CLI is needed for GitHub tools
COPY --from=ghcr.io/cli/cli:latest /usr/bin/gh /usr/bin/gh
ENTRYPOINT ["/triage", "web", "--port", "8080"]
