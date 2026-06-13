## C-Plane — local build & release helpers
## Usage:
##   make build          Build binaries for current OS/arch
##   make release        Cross-compile for linux amd64 + arm64
##   make publish        Create GitHub Release and upload binaries (requires gh CLI)
##   make clean          Remove build artifacts

MODULE   := github.com/ilham/c-plane
REPO     := ilham-fauzi/c-plane
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS  := -s -w -X main.version=$(VERSION)
BIN      := bin

# Current platform build
.PHONY: build
build:
	go build -ldflags '$(LDFLAGS)' -o $(BIN)/cplane          ./cmd/cplane
	go build -ldflags '$(LDFLAGS)' -o $(BIN)/cplane-agent    ./cmd/cplane-agent

# Cross-compile for linux amd64 + arm64
.PHONY: release
release: clean
	GOOS=linux GOARCH=amd64 go build -ldflags '$(LDFLAGS)' -o $(BIN)/cplane-linux-amd64          ./cmd/cplane
	GOOS=linux GOARCH=amd64 go build -ldflags '$(LDFLAGS)' -o $(BIN)/cplane-agent-linux-amd64    ./cmd/cplane-agent
	GOOS=linux GOARCH=arm64 go build -ldflags '$(LDFLAGS)' -o $(BIN)/cplane-linux-arm64          ./cmd/cplane
	GOOS=linux GOARCH=arm64 go build -ldflags '$(LDFLAGS)' -o $(BIN)/cplane-agent-linux-arm64    ./cmd/cplane-agent
	@echo "Built binaries in $(BIN)/ for version $(VERSION)"
	@ls -lh $(BIN)/

# Publish to GitHub Releases (requires: gh auth login)
.PHONY: publish
publish: release
	@if ! command -v gh >/dev/null 2>&1; then \
		echo "Error: gh CLI is required. Install: https://cli.github.com/"; \
		exit 1; \
	fi
	@if [ "$(VERSION)" = "dev" ]; then \
		echo "Error: VERSION is required. Usage: make publish VERSION=v0.1.0"; \
		exit 1; \
	fi
	git tag -a $(VERSION) -m "Release $(VERSION)" 2>/dev/null || true
	git push origin $(VERSION) 2>/dev/null || true
	gh release create $(VERSION) $(BIN)/* \
		--repo $(REPO) \
		--title "$(VERSION)" \
		--notes "Release $(VERSION)" \
		--latest
	@echo "Published $(VERSION) to https://github.com/$(REPO)/releases/tag/$(VERSION)"

.PHONY: test
test:
	go test ./...

.PHONY: clean
clean:
	rm -rf $(BIN)
