FROM golang:1.26-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o apiserverproxy ./cmd/apiserverproxy

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /app/apiserverproxy /apiserverproxy

EXPOSE 8080

ENTRYPOINT ["/apiserverproxy"]
CMD ["--config-file=/etc/apiserverproxy/config.json", "--listen=:8080"]
