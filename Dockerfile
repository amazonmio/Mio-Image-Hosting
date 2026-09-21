# Frontend is embedded into the Go binary; Node and the toolchain stay in build stages.
FROM node:24-bookworm AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
ENV CGO_ENABLED=0
RUN go test ./... \
 && go build -trimpath -ldflags='-s -w' -o /out/mio-image-hosting .

FROM alpine:3.22
RUN adduser -D -H -u 65532 mio \
 && mkdir -p /data \
 && chown mio:mio /data
COPY --from=build /out/mio-image-hosting /usr/local/bin/mio-image-hosting
USER mio
ENV ADDR=0.0.0.0:8080 DATA_DIR=/data
EXPOSE 8080
VOLUME ["/data"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/api/config || exit 1
ENTRYPOINT ["/usr/local/bin/mio-image-hosting"]
