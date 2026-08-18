# Project name
PROJECT_NAME := cs2-analyser-tool

# Build directory
BUILD_DIR := build

# Version injected into the binary: latest tag, or commit hash when untagged
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

# Go parameters
GOCMD := go
GOBUILD := $(GOCMD) build -ldflags "-X github.com/taua-almeida/cs2-analyser-tool/cmd.CS2AnalyserVersion=$(VERSION)"
GOCLEAN := $(GOCMD) clean
GOFILES = $(GOCMD) list -f '{{range .GoFiles}}{{$$.Dir}}/{{.}} {{end}}{{range .CgoFiles}}{{$$.Dir}}/{{.}} {{end}}{{range .IgnoredGoFiles}}{{$$.Dir}}/{{.}} {{end}}{{range .TestGoFiles}}{{$$.Dir}}/{{.}} {{end}}{{range .XTestGoFiles}}{{$$.Dir}}/{{.}} {{end}}' ./...
GOFMT := gofmt
GOTIDY := $(GOCMD) mod tidy
GOTEST := $(GOCMD) test

# Build targets. A target without an explicit architecture defaults to amd64.
BUILD_TARGETS := windows linux darwin darwin-arm64
target_parts = $(subst -, ,$@)
os = $(word 1,$(target_parts))
arch = $(if $(word 2,$(target_parts)),$(word 2,$(target_parts)),amd64)

# Public integration test fixtures. Both are real CS2 demos pinned by SHA-256.
# Sources, licenses and attribution are in analysis/testdata/README.md.
TESTDATA_DIR := analysis/testdata

MIRAGE_DEMO_URL := https://raw.githubusercontent.com/LaihoE/demoparser/4131a4fc02fda291b22421c20e1ca33f149535a7/src/parser/test_demo.dem
MIRAGE_DEMO_SHA256 := 84a1a4191302bdd2a3bbb5a727842093744b1fb1a228aeec630369e44b622cb2

ANCIENT_DEMO_URL := https://api.figshare.com/v2/file/download/52456259
ANCIENT_DEMO_SHA256 := b29a9cb537a181deef97b15cfed10ee722a37999644a27bb2226fdd77a1337fc

# fetch_demo <path> <url> <sha256>. A no-op when the file is already present
# with the right checksum, so it is safe to run repeatedly. The download is
# staged under a *.dem name (covered by .gitignore) and only moved into place
# once it verifies, so an interrupted run cannot leave a corrupt fixture.
# sha256sum is GNU coreutils; macOS ships shasum instead.
define fetch_demo
mkdir -p $(dir $(1)); \
sha() { if command -v sha256sum >/dev/null 2>&1; then sha256sum "$$1"; else shasum -a 256 "$$1"; fi | cut -d' ' -f1; }; \
if [ -f $(1) ] && [ "$$(sha $(1))" = "$(3)" ]; then \
	echo "$(1) already present"; \
else \
	curl -fL --retry 3 -o $(1).part.dem $(2) || { rm -f $(1).part.dem; exit 1; }; \
	got=$$(sha $(1).part.dem); \
	if [ "$$got" != "$(3)" ]; then \
		echo "checksum mismatch for $(2): got $$got, want $(3)" >&2; \
		rm -f $(1).part.dem; exit 1; \
	fi; \
	mv $(1).part.dem $(1); \
	echo "downloaded $(1)"; \
fi
endef

# Ensure GOBIN is not set, which can conflict with cross compilation
unexport GOBIN

.PHONY: build-all check clean download-test-demos fmt fmt-check lint test tidy tidy-check vulncheck $(BUILD_TARGETS)

build-all: $(BUILD_TARGETS)

$(BUILD_TARGETS):
	GOOS=$(os) GOARCH=$(arch) $(GOBUILD) -o '$(BUILD_DIR)/$(PROJECT_NAME)-$(os)-$(arch)' .

test:
	$(GOTEST) ./...

fmt:
	@files="$$($(GOFILES))" || exit $$?; \
	if [ -z "$$files" ]; then echo "No Go package files found." >&2; exit 1; fi; \
	$(GOFMT) -w $$files

fmt-check:
	@files="$$($(GOFILES))" || exit $$?; \
	if [ -z "$$files" ]; then echo "No Go package files found." >&2; exit 1; fi; \
	unformatted="$$($(GOFMT) -l $$files)" || exit $$?; \
	if [ -n "$$unformatted" ]; then \
		echo "Go files must be formatted with gofmt:" >&2; \
		printf '%s\n' "$$unformatted" >&2; \
		echo "Run 'make fmt' and commit the resulting changes." >&2; \
		exit 1; \
	fi

lint:
	$(GOCMD) tool -modfile=tools/go.mod staticcheck ./...

tidy-check:
	$(GOTIDY) -diff
	$(GOCMD) -C tools mod tidy -diff

# Reachability scan of the compiled packages against the Go vulnerability
# database, using the govulncheck version pinned in tools/go.mod. The pinned
# tool is built once for the host, then the scan runs for every released
# platform in BUILD_TARGETS with CGO_ENABLED=0 to match the release builds,
# because an advisory can be reachable only through platform-specific code.
# Each run fetches the current database from https://vuln.go.dev, so the
# result can change without a code change; that is why it is a separate
# target and CI workflow rather than part of check.
vulncheck:
	@mkdir -p $(BUILD_DIR)
	$(GOCMD) -C tools build -o ../$(BUILD_DIR)/govulncheck golang.org/x/vuln/cmd/govulncheck
	@set -e; \
	for target in $(BUILD_TARGETS); do \
		os=$${target%%-*}; \
		arch=$${target#*-}; \
		if [ "$$arch" = "$$target" ]; then arch=amd64; fi; \
		echo "govulncheck $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch ./$(BUILD_DIR)/govulncheck ./...; \
	done

check: fmt-check tidy-check lint test

# Fetch the demos the integration test runs against.
download-test-demos:
	@$(call fetch_demo,$(TESTDATA_DIR)/mirage.dem,$(MIRAGE_DEMO_URL),$(MIRAGE_DEMO_SHA256))
	@$(call fetch_demo,$(TESTDATA_DIR)/ancient.dem,$(ANCIENT_DEMO_URL),$(ANCIENT_DEMO_SHA256))

tidy:
	$(GOTIDY)
	$(GOCMD) -C tools mod tidy

clean:
	$(GOCLEAN)
	rm -f $(BUILD_DIR)/*
	rm -rf ./dist
