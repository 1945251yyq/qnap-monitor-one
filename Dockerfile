# syntax=docker/dockerfile:1.7
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS TARGETARCH VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build \
    -trimpath -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/qnap-status-lite ./cmd/qnap-status-lite

FROM alpine:3.23
RUN addgroup -S qnap && adduser -S -G qnap qnap
COPY --from=build /out/qnap-status-lite /usr/local/bin/qnap-status-lite
EXPOSE 8080
VOLUME ["/data"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD wget -q -O - http://127.0.0.1:8080/api/health >/dev/null || exit 1
ENTRYPOINT ["/usr/local/bin/qnap-status-lite"]
