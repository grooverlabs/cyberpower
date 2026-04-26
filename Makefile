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

.PHONY: all build clean test check package help generate

help:
	@echo "Usage:"
	@echo "  make build               - Build for native platform (runs generate first)"
	@echo "  make build ARCH=arm64    - Cross-compile for ARM64"
	@echo "  make build ARCH=amd64    - Cross-compile for AMD64"
	@echo "  make package ARCH=arm64  - Build .deb package (implies build)"
	@echo "  make generate            - Run templ generate for views"
	@echo "  make test                - Run go tests"
	@echo "  make check               - Run go vet"
	@echo "  make clean               - Remove build artifacts (incl. generated views)"

# Default target
all: build

# Generate templ views. Prints a friendly hint if templ isn't installed.
generate:
	@command -v templ >/dev/null 2>&1 || { \
	  echo "error: templ CLI not found"; \
	  echo "install with: go install github.com/a-h/templ/cmd/templ@latest"; \
	  exit 1; }
	templ generate ./...

# Build for specified architecture (or native if ARCH not set)
build: generate
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

# Remove build artifacts and generated views
clean:
	rm -rf $(BUILD_DIR)
	find views -name '*_templ.go' -delete 2>/dev/null || true
