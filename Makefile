.PHONY: build site site-release test vet verify clean

VERSION ?= dev
LDFLAGS := -s -w -X main.version=$(VERSION)
BINARIES := annolux-api annolux-index annolux-mcp annolux-healthcheck annolux-admin annolux-indexclean

build:
	mkdir -p bin
	@for name in $(BINARIES); do \
		go build -trimpath -ldflags "$(LDFLAGS)" -o "bin/$$name" "./cmd/$$name"; \
	done

site:
	go run ./tools/sitegen

site-release:
	go run ./tools/sitegen --release

test:
	go test -race -count=1 ./...

vet:
	go vet ./...

verify: site
	bash scripts/repo-policy.sh
	bash scripts/mcp-distribution-check.sh
	bash scripts/site-check.sh
	go run ./tools/rankinglock
	go test ./...
	go vet ./...
	git diff --check

clean:
	rm -rf bin
