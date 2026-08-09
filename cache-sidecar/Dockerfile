# Build: docker build -f cache-sidecar/Dockerfile -t gha-cache-sidecar:local .
FROM golang:1.23-alpine AS build
RUN apk add --no-cache git ca-certificates
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/sidecar ./cache-sidecar/cmd/sidecar

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/sidecar /sidecar
USER nonroot:nonroot
ENTRYPOINT ["/sidecar"]
