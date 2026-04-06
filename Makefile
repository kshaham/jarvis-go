VERSION ?= 0.2.0-go
LDFLAGS := -s -w -X main.version=$(VERSION)
BINARY := jarvis

.PHONY: build build-all test test-race bench clean

build:
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o $(BINARY) ./cmd/jarvis/

build-all:
	@mkdir -p dist
	@for os_arch in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do \
		GOOS=$${os_arch%/*} GOARCH=$${os_arch#*/} CGO_ENABLED=0 \
		go build -ldflags="$(LDFLAGS)" \
		-o dist/$(BINARY)-$${os_arch%/*}-$${os_arch#*/} \
		./cmd/jarvis/; \
		echo "Built dist/$(BINARY)-$${os_arch%/*}-$${os_arch#*/}"; \
	done

test:
	go test ./... -count=1 -timeout=60s

test-race:
	go test ./... -race -count=1 -timeout=60s

bench:
	go test ./... -bench=. -benchmem -count=1

clean:
	rm -rf $(BINARY) dist/
