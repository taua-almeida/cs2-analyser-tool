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

# Integration test fixture: a real CS2 Premier demo (de_mirage) from the
# MIT-licensed LaihoE/demoparser test data, pinned to an immutable commit.
# The SHA-256 here must match the one pinned in cmd/demo_parser/integration_test.go.
TEST_DEMO_URL := https://raw.githubusercontent.com/LaihoE/demoparser/4131a4fc02fda291b22421c20e1ca33f149535a7/src/parser/test_demo.dem
TEST_DEMO_SHA256 := 84a1a4191302bdd2a3bbb5a727842093744b1fb1a228aeec630369e44b622cb2
TEST_DEMO_PATH := cmd/demo_parser/testdata/test_demo.dem
# Staged under a *.dem name so the repo's existing ignore rule covers it.
TEST_DEMO_TMP := cmd/demo_parser/testdata/test_demo.part.dem

# Ensure GOBIN is not set, which can conflict with cross compilation
unexport GOBIN

.PHONY: build-all clean tidy test download-test-demo $(PLATFORMS)

build-all: windows linux darwin

$(PLATFORMS):
	GOOS=$(os) GOARCH=amd64 $(GOBUILD) -o '$(BUILD_DIR)/$(PROJECT_NAME)-$(os)-amd64' .

test:
	$(GOTEST) ./...

# Fetch the integration test fixture demo. A no-op when the file is already
# present with the right checksum, so it is safe to run repeatedly. The
# download is staged and only moved into place once it verifies, so an
# interrupted run can never leave a corrupt fixture behind. sha256sum is
# GNU coreutils; macOS ships shasum instead.
download-test-demo:
	@mkdir -p $(dir $(TEST_DEMO_PATH))
	@sha() { \
		if command -v sha256sum >/dev/null 2>&1; then sha256sum "$$1"; else shasum -a 256 "$$1"; fi | cut -d' ' -f1; \
	}; \
	if [ -f $(TEST_DEMO_PATH) ] && [ "$$(sha $(TEST_DEMO_PATH))" = "$(TEST_DEMO_SHA256)" ]; then \
		echo "$(TEST_DEMO_PATH) already present"; \
	else \
		curl -fL --retry 3 -o $(TEST_DEMO_TMP) $(TEST_DEMO_URL) || { rm -f $(TEST_DEMO_TMP); exit 1; }; \
		got=$$(sha $(TEST_DEMO_TMP)); \
		if [ "$$got" != "$(TEST_DEMO_SHA256)" ]; then \
			echo "checksum mismatch for $(TEST_DEMO_URL): got $$got, want $(TEST_DEMO_SHA256)" >&2; \
			rm -f $(TEST_DEMO_TMP); exit 1; \
		fi; \
		mv $(TEST_DEMO_TMP) $(TEST_DEMO_PATH); \
		echo "downloaded $(TEST_DEMO_PATH)"; \
	fi

tidy:
	$(GOTIDY)

clean:
	$(GOCLEAN)
	rm -f $(BUILD_DIR)/*
