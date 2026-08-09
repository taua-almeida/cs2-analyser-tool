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

# Ensure GOBIN is not set, which can conflict with cross compilation
unexport GOBIN

.PHONY: build-all clean tidy test download-test-demo $(PLATFORMS)

build-all: windows linux darwin

$(PLATFORMS):
	GOOS=$(os) GOARCH=amd64 $(GOBUILD) -o '$(BUILD_DIR)/$(PROJECT_NAME)-$(os)-amd64' .

test:
	$(GOTEST) ./...

# Fetch the integration test fixture demo. Skipped when the file is already
# present with the right checksum, so it is safe to run repeatedly.
download-test-demo:
	@mkdir -p $(dir $(TEST_DEMO_PATH))
	@if echo "$(TEST_DEMO_SHA256)  $(TEST_DEMO_PATH)" | sha256sum -c --status 2>/dev/null; then \
		echo "$(TEST_DEMO_PATH) already present"; \
	else \
		curl -fL --retry 3 -o $(TEST_DEMO_PATH) $(TEST_DEMO_URL) && \
		echo "$(TEST_DEMO_SHA256)  $(TEST_DEMO_PATH)" | sha256sum -c; \
	fi

tidy:
	$(GOTIDY)

clean:
	$(GOCLEAN)
	rm -f $(BUILD_DIR)/*
