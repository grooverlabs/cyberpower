# Binaries
CLI_BINARY=ups-cli
MONITOR_BINARY=ups-monitor

# Directories
CLI_DIR=./cmd/cli
MONITOR_DIR=./cmd/monitor
BUILD_DIR=bin

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test

# Default architecture (arm64 for Pi5, use ARCH=amd64 for Intel)
ARCH ?= arm64
VERSION ?= 0.0.1

.PHONY: all build clean test check cross-compile package help

help:
	@echo "Usage:"
	@echo "  make build           - Build for the current platform"
	@echo "  make cross-compile   - Cross-compile for ARCH (default arm64)"
	@echo "  make package         - Build .deb package for ARCH (default arm64)"
	@echo "  make test            - Run go tests"
	@echo "  make check           - Run go vet"
	@echo "  make clean           - Remove build artifacts"

# Default target
all: build

# Build for current platform
build:
	mkdir -p $(BUILD_DIR)
	$(GOBUILD) -o $(BUILD_DIR)/$(CLI_BINARY) $(CLI_DIR)
	$(GOBUILD) -o $(BUILD_DIR)/$(MONITOR_BINARY) $(MONITOR_DIR)

# Cross-compile for target architecture
cross-compile:
	mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=$(ARCH) $(GOBUILD) -o $(BUILD_DIR)/$(CLI_BINARY)-$(ARCH) $(CLI_DIR)
	GOOS=linux GOARCH=$(ARCH) $(GOBUILD) -o $(BUILD_DIR)/$(MONITOR_BINARY)-$(ARCH) $(MONITOR_DIR)

# Build .deb package (requires nfpm)
package: cross-compile
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
	rm -rf $(BUILD_DIR) *.deb
