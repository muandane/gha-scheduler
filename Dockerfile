# Build: docker build -f Dockerfile -t gha-scheduler:local .
FROM node:22-alpine AS web
RUN corepack enable && corepack prepare pnpm@9.15.4 --activate
WORKDIR /web
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build
RUN cp -R dist /console-static

FROM golang:1.26-alpine AS build
RUN apk add --no-cache git ca-certificates
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY --from=web /console-static ./internal/console/static
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/scheduler ./cmd/scheduler

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/scheduler /scheduler
USER nonroot:nonroot
ENTRYPOINT ["/scheduler"]
