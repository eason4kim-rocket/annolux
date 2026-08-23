.PHONY: build test vet clean

VERSION ?= dev
LDFLAGS := -s -w -X main.version=$(VERSION)
BINARIES := annolux-api annolux-index annolux-mcp annolux-healthcheck annolux-admin annolux-indexclean

build:
	mkdir -p bin
	@for name in $(BINARIES); do \
		go build -trimpath -ldflags "$(LDFLAGS)" -o "bin/$$name" "./cmd/$$name"; \
	done



test:
	go test -race -count=1 ./...

vet:
	go vet ./...


clean:
	rm -rf bin
