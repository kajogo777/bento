FROM golang:1.23-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags "-s -w" -o /bento ./cmd/bento

FROM alpine:3.20
RUN apk add --no-cache ca-certificates git
COPY --from=builder /bento /usr/local/bin/bento
ENTRYPOINT ["bento"]
