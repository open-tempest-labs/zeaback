BINARY  := zeaback
CMD     := ./cmd/zeaback
TAGS    := duckdb_arrow
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

.PHONY: build build-volumez install test tidy clean docker-build docker-test docker-down

build:
	go build -tags $(TAGS) $(LDFLAGS) -o $(BINARY) $(CMD)

# Opt-in build with the volumez storage integration. Requires the volumez module
# to be resolvable — see the "Combining with volumez" section of the README for
# the go.work setup.
build-volumez:
	go build -tags "$(TAGS) volumez" $(LDFLAGS) -o $(BINARY) $(CMD)

install:
	go install -tags $(TAGS) $(LDFLAGS) $(CMD)

test:
	go test -tags $(TAGS) ./...

# The volumez adapter is an optional, build-tagged integration whose upstream
# module path is being reconciled; -e keeps tidy from failing on that opt-in
# import when the volumez module is not present.
tidy:
	go mod tidy -e

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
