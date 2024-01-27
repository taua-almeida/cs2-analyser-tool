# Project name
PROJECT_NAME := cs2-analyser-tool

# Build directory
BUILD_DIR := build

# Go parameters
GOCMD := go
GOBUILD := $(GOCMD) build
GOCLEAN := $(GOCMD) clean
GOTIDY := $(GOCMD) mod tidy

# Platforms
PLATFORMS := windows linux darwin
os = $(word 1, $@)

# Ensure GOBIN is not set, which can conflict with cross compilation
unexport GOBIN

.PHONY: build-all clean tidy

build-all: windows linux darwin

$(PLATFORMS):
	GOOS=$(os) GOARCH=amd64 $(GOBUILD) -o '$(BUILD_DIR)/$(PROJECT_NAME)-$(os)-amd64' .

tidy:
	$(GOTIDY)

clean:
	$(GOCLEAN)
	rm -f $(BUILD_DIR)/*

