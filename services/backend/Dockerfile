FROM golang:1.25.1-alpine AS build
WORKDIR /src
COPY services/backend/go.mod services/backend/go.sum ./
RUN go mod download
COPY services/backend/ ./
RUN CGO_ENABLED=0 go build -trimpath -o /out/ ./cmd/...

FROM alpine:3.22
RUN apk add --no-cache ca-certificates && addgroup -g 10001 app && adduser -D -u 10001 -G app app && mkdir -p /data/files && chown -R app:app /data
WORKDIR /app
COPY --from=build /out/ /app/
USER app
EXPOSE 8080
CMD ["/app/api"]

