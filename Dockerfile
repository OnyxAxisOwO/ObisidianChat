FROM node:24-alpine AS frontend
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.27-alpine AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY web/embed.go ./web/embed.go
COPY --from=frontend /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/obsidianchat ./cmd/obsidianchat

FROM alpine:3.23
RUN addgroup -g 10001 chat && adduser -D -u 10001 -G chat chat && mkdir /data && chown chat:chat /data
COPY --from=backend /out/obsidianchat /usr/local/bin/obsidianchat
USER 10001:10001
WORKDIR /data
ENV OC_ADDR=0.0.0.0:8090 OC_DATABASE=/data/chat.db
EXPOSE 8090
VOLUME /data
HEALTHCHECK --interval=30s --timeout=3s CMD wget -q -O /dev/null http://127.0.0.1:8090/healthz || exit 1
ENTRYPOINT ["obsidianchat"]
