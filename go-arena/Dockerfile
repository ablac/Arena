FROM golang:1.25 AS builder
WORKDIR /app
COPY go-arena/go.mod go-arena/go.sum ./go-arena/
RUN cd go-arena && go mod download
COPY go-arena/ ./go-arena/
RUN cd go-arena && CGO_ENABLED=0 go build -o /arena-server ./cmd/arena-server

FROM gcr.io/distroless/static
COPY --from=builder /arena-server /arena-server
COPY frontend/ /frontend/
EXPOSE 8000
CMD ["/arena-server"]
