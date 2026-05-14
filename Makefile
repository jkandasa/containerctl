BINARY     := containerctl
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
BUILD_DATE ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
PKG        := github.com/jkandasa/containerctl/cmd
LDFLAGS    := -ldflags "-s -w \
  -X $(PKG).Version=$(VERSION) \
  -X $(PKG).BuildDate=$(BUILD_DATE)"
GOFLAGS  := -trimpath

.PHONY: build clean lint test cross-build release

build:
	go build $(GOFLAGS) $(LDFLAGS) -o $(BINARY) .

test:
	go test ./...

lint:
	golangci-lint run ./...

clean:
	rm -f $(BINARY)
	rm -rf dist/

# Usage: make release REL_VERSION=v1.2.0
release:
	@if [ -z "$(REL_VERSION)" ]; then echo "Usage: make release REL_VERSION=v1.2.0"; exit 1; fi
	@if git rev-parse "$(REL_VERSION)" >/dev/null 2>&1; then echo "Tag $(REL_VERSION) already exists"; exit 1; fi
	@grep -q "^## \[$(REL_VERSION)\]" CHANGELOG.md && { echo "$(REL_VERSION) already in CHANGELOG.md"; exit 1; } || true
	bash scripts/update-changelog.sh $(REL_VERSION)
	git add CHANGELOG.md
	git commit -m "chore: release $(REL_VERSION)"
	git tag $(REL_VERSION)
	git push origin main
	git push origin $(REL_VERSION)
	@echo "Done — $(REL_VERSION) is live"

cross-build:
	mkdir -p dist
	GOOS=linux   GOARCH=amd64 go build $(GOFLAGS) $(LDFLAGS) -o dist/$(BINARY)-linux-amd64 .
	GOOS=linux   GOARCH=arm64 go build $(GOFLAGS) $(LDFLAGS) -o dist/$(BINARY)-linux-arm64 .
	GOOS=linux   GOARCH=arm   GOARM=7 go build $(GOFLAGS) $(LDFLAGS) -o dist/$(BINARY)-linux-armv7 .
	GOOS=windows GOARCH=amd64 go build $(GOFLAGS) $(LDFLAGS) -o dist/$(BINARY)-windows-amd64.exe .
	GOOS=windows GOARCH=arm64 go build $(GOFLAGS) $(LDFLAGS) -o dist/$(BINARY)-windows-arm64.exe .
	GOOS=darwin  GOARCH=amd64 go build $(GOFLAGS) $(LDFLAGS) -o dist/$(BINARY)-darwin-amd64 .
	GOOS=darwin  GOARCH=arm64 go build $(GOFLAGS) $(LDFLAGS) -o dist/$(BINARY)-darwin-arm64 .
