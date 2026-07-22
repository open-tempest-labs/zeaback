BINARY  := zeaback
CMD     := ./cmd/zeaback
TAGS    := duckdb_arrow
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

.PHONY: build install test tidy clean docker-build docker-test docker-down

build:
	go build -tags $(TAGS) $(LDFLAGS) -o $(BINARY) $(CMD)

install:
	go install -tags $(TAGS) $(LDFLAGS) $(CMD)

test:
	go test -tags $(TAGS) ./...

tidy:
	go mod tidy

clean:
	rm -f $(BINARY)

# Build the runtime Docker image
docker-build:
	docker build --target runtime --build-arg VERSION=$(VERSION) -t zeaback:$(VERSION) .

# Run the full test suite inside Docker (with MinIO for S3 integration tests)
docker-test:
	docker compose run --rm test

# Tear down test infrastructure
docker-down:
	docker compose down -v
