# syntax=docker/dockerfile:1
# One image for the whole app:
#   cv-api (default)  the API, which also serves the web app (API under /api)
#   cv-api migrate    runs database migrations and exits; once per release
#   cv-worker         the background worker
#
# The web build and the Go build run on the build machine's platform: the
# web output is the same everywhere, and Go cross-compiles.

FROM --platform=$BUILDPLATFORM node:24-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.27.4-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG TARGETOS TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/cv-api ./cmd/api && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/cv-worker ./cmd/worker

# Static binaries need no libc: distroless has CA certificates and a nonroot
# user, and no shell. Uploaded PDFs are parsed in a child process that runs
# this same binary (cv-api extract-pdf), so nothing else is needed.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ /usr/local/bin/
COPY --from=web /web/dist/ /usr/share/cv-evaluator/web/

ARG SERVICE_VERSION=dev
ENV SERVICE_VERSION=$SERVICE_VERSION \
    WEB_DIR=/usr/share/cv-evaluator/web

USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/cv-api"]
