.PHONY: all build clean test run dbuild dup ddown dlogs dclean test-client

BINARY_NAME=timeflux
BIN_DIR=bin
BUILD_FLAGS=-v
DOCKER_IMAGE=timeflux:latest

all: build

build:
	@mkdir -p $(BIN_DIR)
	go build $(BUILD_FLAGS) -o $(BIN_DIR)/$(BINARY_NAME)

testclient:
	@mkdir -p $(BIN_DIR)
	go build $(BUILD_FLAGS) -o $(BIN_DIR)/testclient ./cmd/testclient

clean:
	@rm -rf $(BIN_DIR)
	go clean

test:
	go test -v ./...

run: build
	$(BIN_DIR)/$(BINARY_NAME)

reup:
	docker-compose up -d --build timeflux

up:
	docker-compose up -d

down:
	docker-compose down

logs:
	docker-compose logs -f timeflux

dcl:
	docker-compose down -v
