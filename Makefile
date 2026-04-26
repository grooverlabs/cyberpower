# Binaries
CLI_BINARY=ups-cli
MONITOR_BINARY=ups-monitor

# Directories
CLI_DIR=./cmd/ups-cli
BUILD_DIR=dist

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test

# Default architecture (arm64 for Pi5, use ARCH=amd64 for Intel)
# Set both GOOS and GOARCH if cross-compiling, otherwise use host defaults
ARCH ?= $(shell go env GOARCH)
OS ?= $(shell go env GOOS)
VERSION ?= 0.0.1

.PHONY: all build clean test check package help

help:
	@echo "Usage:"
	@echo "  make build               - Build for native platform"
	@echo "  make build ARCH=arm64    - Cross-compile for ARM64"
	@echo "  make build ARCH=amd64    - Cross-compile for AMD64"
	@echo "  make package ARCH=arm64  - Build .deb package (implies build)"
	@echo "  make test                - Run go tests"
	@echo "  make check               - Run go vet"
	@echo "  make clean               - Remove build artifacts"

# Default target
all: build

# Build for specified architecture (or native if ARCH not set)
build:
	mkdir -p $(BUILD_DIR)
	GOOS=$(OS) GOARCH=$(ARCH) $(GOBUILD) -o $(BUILD_DIR)/$(MONITOR_BINARY)-$(ARCH) -v .
	GOOS=$(OS) GOARCH=$(ARCH) $(GOBUILD) -o $(BUILD_DIR)/$(CLI_BINARY)-$(ARCH) -v $(CLI_DIR)

# Build .deb package (requires nfpm)
package: build
	cp $(BUILD_DIR)/$(CLI_BINARY)-$(ARCH) $(BUILD_DIR)/$(CLI_BINARY)-pkg
	cp $(BUILD_DIR)/$(MONITOR_BINARY)-$(ARCH) $(BUILD_DIR)/$(MONITOR_BINARY)-pkg
	ARCH=$(ARCH) VERSION=$(VERSION) nfpm pkg --packager deb --target $(BUILD_DIR)/cyberpower-ups_$(VERSION)_$(ARCH).deb
	rm -f $(BUILD_DIR)/$(CLI_BINARY)-pkg $(BUILD_DIR)/$(MONITOR_BINARY)-pkg

# Run tests
test:
	$(GOTEST) ./...

# Basic lint/vet check
check:
	go vet ./...

# Remove build artifacts
clean:
	rm -rf $(BUILD_DIR)
