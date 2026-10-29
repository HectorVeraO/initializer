ARG GO_VERSION=1.26.10

FROM golang:${GO_VERSION}-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/server ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot AS runtime

COPY --from=builder /out/server /usr/local/bin/server
ENTRYPOINT ["/usr/local/bin/server"]
