# Nazar engine. Build context is the repo root, because the binary resolves the feature
# registry, rule bundles, policy and model bundle from NAZAR_REPO_ROOT at startup.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go/go.mod go/go.sum ./
RUN go mod download
COPY go/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/nazar ./cmd/nazar

# Grab the docker CLI so the chaos endpoint can stop/start sibling containers.
FROM docker:27-cli AS docker-cli

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/nazar /app/nazar
COPY --from=docker-cli /usr/local/bin/docker /usr/local/bin/docker
# Runtime-resolved assets, all read-only at runtime.
COPY features            /app/features
COPY rules               /app/rules
COPY policy              /app/policy
COPY py/training/output  /app/py/training/output
COPY py/eval/output      /app/py/eval/output
# The write-ahead log is the only thing the engine writes to disk; keep it on a volume so
# the image layers stay immutable.
ENV NAZAR_REPO_ROOT=/app \
    NAZAR_DATA_DIR=/data
VOLUME ["/data"]
EXPOSE 8080
USER nonroot
ENTRYPOINT ["/app/nazar"]
