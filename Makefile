BIN := urd
DIST := dist
PLATFORMS := darwin/arm64 darwin/amd64 linux/amd64 linux/arm64

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null)
# Overridable: the timestamp is the one thing keeping two builds of a commit from being
# identical, so "make dist DATE=..." reproduces one.
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

.PHONY: build test lint bench clean dist demo

# The same stamp as dist, or a locally installed binary reports "dev".
build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/urd

test:
	go test ./...

lint:
	go vet ./...

bench:
	go test -bench=. -benchmem ./internal/query/...

# A GIF, not the .cast: GitHub does not embed the asciinema player.
demo:
	go run ./tools/democast
	@if command -v agg >/dev/null 2>&1; then \
		agg demo/urd.cast demo/urd.gif; \
		echo "GIF: demo/urd.gif ($$(du -h demo/urd.gif | cut -f1))"; \
	else \
		echo "agg not found: install with 'brew install agg' (or 'cargo install --locked agg'), then run 'agg demo/urd.cast demo/urd.gif'"; \
	fi

clean:
	rm -f $(BIN)

# COPYFILE_DISABLE and --no-xattrs: packed on macOS, these archives otherwise carry
# com.apple.provenance, and GNU tar on the Linux side prints a warning per file.
dist:
	rm -rf $(DIST)
	mkdir -p $(DIST)
	@for platform in $(PLATFORMS); do \
		os=$${platform%/*}; arch=$${platform#*/}; \
		name=urd_$(VERSION)_$${os}_$${arch}; \
		stage=$(DIST)/$$name; \
		mkdir -p $$stage; \
		echo "building $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" -o $$stage/urd ./cmd/urd || exit 1; \
		cp README.md $$stage/; \
		cp packaging/uninstall.sh $$stage/; \
		files="urd README.md uninstall.sh"; \
		if [ -f LICENSE ]; then cp LICENSE $$stage/; files="$$files LICENSE"; fi; \
		COPYFILE_DISABLE=1 tar --no-xattrs -C $$stage -czf $(DIST)/$$name.tar.gz $$files; \
		rm -rf $$stage; \
	done
	@cd $(DIST) && \
		if command -v sha256sum >/dev/null 2>&1; then \
			sha256sum *.tar.gz > checksums.txt; \
		else \
			shasum -a 256 *.tar.gz > checksums.txt; \
		fi
