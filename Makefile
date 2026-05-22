BINARY     := containerctl
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
BUILD_DATE ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
PKG        := github.com/jkandasa/containerctl/cmd
LDFLAGS    := -ldflags "-s -w \
  -X $(PKG).Version=$(VERSION) \
  -X $(PKG).BuildDate=$(BUILD_DATE)"
GOFLAGS  := -trimpath

XTERM_VERSION := 5.3.0
CM_VERSION    := 5.65.17
CM_CDN        := https://cdnjs.cloudflare.com/ajax/libs/codemirror/$(CM_VERSION)

# All third-party assets that must be embedded in the binary.
# Declared as file targets so make only fetches what is missing.
ASSET_DIR := internal/web/assets
ASSETS    := \
	$(ASSET_DIR)/xterm.js \
	$(ASSET_DIR)/xterm.css \
	$(ASSET_DIR)/xterm-addon-fit.js \
	$(ASSET_DIR)/codemirror.js \
	$(ASSET_DIR)/codemirror.css \
	$(ASSET_DIR)/codemirror-dialog.js \
	$(ASSET_DIR)/codemirror-dialog.css \
	$(ASSET_DIR)/codemirror-vim.js \
	$(ASSET_DIR)/codemirror-yaml.js

.PHONY: build clean clean-assets lint test cross-build release assets

# build always ensures third-party assets are present before compiling so the
# resulting binary can serve the web terminal fully offline.
build: $(ASSETS)
	go build $(GOFLAGS) $(LDFLAGS) -o $(BINARY) .

test:
	go test ./...

lint:
	golangci-lint run ./...

# assets force-refreshes every third-party file regardless of whether it exists.
assets: clean-assets $(ASSETS)

# ── individual asset file targets (downloaded only when the file is absent) ──

$(ASSET_DIR)/xterm.js:
	curl -fsSL https://cdn.jsdelivr.net/npm/xterm@$(XTERM_VERSION)/lib/xterm.min.js -o $@

$(ASSET_DIR)/xterm.css:
	curl -fsSL https://cdn.jsdelivr.net/npm/xterm@$(XTERM_VERSION)/css/xterm.css -o $@

$(ASSET_DIR)/xterm-addon-fit.js:
	curl -fsSL https://cdn.jsdelivr.net/npm/xterm-addon-fit@0.8.0/lib/xterm-addon-fit.min.js -o $@

$(ASSET_DIR)/codemirror.js:
	curl -fsSL $(CM_CDN)/codemirror.min.js -o $@

$(ASSET_DIR)/codemirror.css:
	curl -fsSL $(CM_CDN)/codemirror.min.css -o $@

$(ASSET_DIR)/codemirror-dialog.js:
	curl -fsSL $(CM_CDN)/addon/dialog/dialog.min.js -o $@

$(ASSET_DIR)/codemirror-dialog.css:
	curl -fsSL $(CM_CDN)/addon/dialog/dialog.min.css -o $@

$(ASSET_DIR)/codemirror-vim.js:
	curl -fsSL $(CM_CDN)/keymap/vim.min.js -o $@

$(ASSET_DIR)/codemirror-yaml.js:
	curl -fsSL $(CM_CDN)/mode/yaml/yaml.min.js -o $@

# ── cleanup ───────────────────────────────────────────────────────────────────

clean:
	rm -f $(BINARY)
	rm -rf dist/

clean-assets:
	rm -f $(ASSETS)

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

cross-build: $(ASSETS)
	mkdir -p dist
	GOOS=linux   GOARCH=amd64 go build $(GOFLAGS) $(LDFLAGS) -o dist/$(BINARY)-linux-amd64 .
	GOOS=linux   GOARCH=arm64 go build $(GOFLAGS) $(LDFLAGS) -o dist/$(BINARY)-linux-arm64 .
	GOOS=linux   GOARCH=arm   GOARM=7 go build $(GOFLAGS) $(LDFLAGS) -o dist/$(BINARY)-linux-armv7 .
	GOOS=windows GOARCH=amd64 go build $(GOFLAGS) $(LDFLAGS) -o dist/$(BINARY)-windows-amd64.exe .
	GOOS=windows GOARCH=arm64 go build $(GOFLAGS) $(LDFLAGS) -o dist/$(BINARY)-windows-arm64.exe .
	GOOS=darwin  GOARCH=amd64 go build $(GOFLAGS) $(LDFLAGS) -o dist/$(BINARY)-darwin-amd64 .
	GOOS=darwin  GOARCH=arm64 go build $(GOFLAGS) $(LDFLAGS) -o dist/$(BINARY)-darwin-arm64 .
