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
GOTIDY := $(GOCMD) mod tidy
GOTEST := $(GOCMD) test

# Platforms
PLATFORMS := windows linux darwin
os = $(word 1, $@)

# Integration test fixtures. Both are real CS2 demos pinned by SHA-256; the
# checksums here must match the ones in cmd/demo_parser/integration_test.go,
# which explains what each demo is there to cover.
TESTDATA_DIR := cmd/demo_parser/testdata

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

.PHONY: build-all clean tidy test download-test-demos $(PLATFORMS)

build-all: windows linux darwin

$(PLATFORMS):
	GOOS=$(os) GOARCH=amd64 $(GOBUILD) -o '$(BUILD_DIR)/$(PROJECT_NAME)-$(os)-amd64' .

test:
	$(GOTEST) ./...

# Fetch the demos the integration test runs against.
download-test-demos:
	@$(call fetch_demo,$(TESTDATA_DIR)/mirage.dem,$(MIRAGE_DEMO_URL),$(MIRAGE_DEMO_SHA256))
	@$(call fetch_demo,$(TESTDATA_DIR)/ancient.dem,$(ANCIENT_DEMO_URL),$(ANCIENT_DEMO_SHA256))

tidy:
	$(GOTIDY)

clean:
	$(GOCLEAN)
	rm -f $(BUILD_DIR)/*
