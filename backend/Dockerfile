# Build context is the repo root so the image can ship api/openapi.yaml.
FROM golang:1.27.4-alpine3.24 AS build
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/app ./cmd/app

FROM alpine:3.24.4
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 app
COPY --from=build /out/app /usr/local/bin/app
COPY api/openapi.yaml /app/openapi.yaml
USER app
EXPOSE 8080
ENTRYPOINT ["app"]
CMD ["serve"]
