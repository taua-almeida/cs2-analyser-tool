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

# Ensure GOBIN is not set, which can conflict with cross compilation
unexport GOBIN

.PHONY: build-all clean tidy test $(PLATFORMS)

build-all: windows linux darwin

$(PLATFORMS):
	GOOS=$(os) GOARCH=amd64 $(GOBUILD) -o '$(BUILD_DIR)/$(PROJECT_NAME)-$(os)-amd64' .

test:
	$(GOTEST) ./...

tidy:
	$(GOTIDY)

clean:
	$(GOCLEAN)
	rm -f $(BUILD_DIR)/*
