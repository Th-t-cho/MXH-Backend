FROM --platform=linux/amd64 golang:1.25-bookworm AS gobuilder
WORKDIR /backend
ENV CGO_ENABLED=1

ENV GOPROXY=https://proxy.golang.org,direct
RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc libwebp-dev \
    && rm -rf /var/lib/apt/lists/*
RUN go clean -modcache

COPY . .
RUN go mod tidy
RUN mkdir -p /out/

ARG TARGETOS
ARG TARGETARCH
RUN GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -buildvcs=false -o /out/server ./cmd/server

FROM debian:bookworm-slim AS bin
WORKDIR /app
RUN apt-get update && apt-get install -y --no-install-recommends \
    libwebp7 ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=gobuilder /out/server server

CMD ["./server"]
