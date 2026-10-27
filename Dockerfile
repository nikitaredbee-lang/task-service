FROM golang:1.24-alpine AS build

WORKDIR /src

# сначала зависимости чтобы слой кешировался
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/task-service ./cmd/api


FROM alpine:3.22

RUN adduser -D -H -u 10001 app
COPY --from=build /out/task-service /usr/local/bin/task-service

USER app
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/task-service"]
