.PHONY: build run clean test

# Binary name
BINARY := apiserverproxy

# Build directory
BUILD_DIR := ./bin

# Default API server URL (can be overridden)
API_SERVER_URL ?= https://localhost:6443
LISTEN_ADDR ?= :8080

build:
	go build -o $(BUILD_DIR)/$(BINARY) ./cmd/apiserverproxy

run: build
	$(BUILD_DIR)/$(BINARY) --api-server-url=$(API_SERVER_URL) --listen=$(LISTEN_ADDR)

run-dev:
	go run ./cmd/apiserverproxy --api-server-url=$(API_SERVER_URL) --listen=$(LISTEN_ADDR)

test:
	go test -race ./...

clean:
	rm -rf $(BUILD_DIR)

install: build
	cp $(BUILD_DIR)/$(BINARY) /usr/local/bin/