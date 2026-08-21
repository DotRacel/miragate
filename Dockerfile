# syntax=docker/dockerfile:1

# ---- build ----
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/miragate ./cmd/miragate

# ---- runtime ----
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 10001 app
COPY --from=build /out/miragate /usr/local/bin/miragate
USER app
WORKDIR /data
ENV MIRAGATE_HOME=/data
EXPOSE 8788
ENTRYPOINT ["miragate"]
CMD ["serve", "--listen", "0.0.0.0:8788"]
