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

.PHONY: all build clean test check package help generate tailwind tailwind-watch tools-check

help:
	@echo "Usage:"
	@echo "  make build               - Build for native platform (runs generate first)"
	@echo "  make build ARCH=arm64    - Cross-compile for ARM64"
	@echo "  make build ARCH=amd64    - Cross-compile for AMD64"
	@echo "  make package ARCH=arm64  - Build .deb package (implies build)"
	@echo "  make generate            - Run tailwind + templ generate"
	@echo "  make tailwind            - Compile assets/static/css/app.src.css"
	@echo "  make tailwind-watch      - Watch and recompile CSS on changes"
	@echo "  make test                - Run go tests"
	@echo "  make check               - Run go vet"
	@echo "  make clean               - Remove build artifacts (incl. generated views/CSS)"

# Default target
all: build

# Verify required external tools are installed.
tools-check:
	@command -v templ >/dev/null 2>&1 || { \
	  echo "error: templ CLI not found"; \
	  echo "install with: go install github.com/a-h/templ/cmd/templ@latest"; \
	  exit 1; }
	@command -v tailwindcss >/dev/null 2>&1 || { \
	  echo "error: tailwindcss CLI not found"; \
	  echo "download standalone: https://github.com/tailwindlabs/tailwindcss/releases"; \
	  exit 1; }

# Compile Tailwind CSS once.
tailwind: tools-check
	tailwindcss -i assets/static/css/app.src.css -o assets/static/css/app.css --minify

# Watch mode for local development (use alongside 'go run .').
tailwind-watch:
	tailwindcss -i assets/static/css/app.src.css -o assets/static/css/app.css --watch

# Generate everything that goes into the build: CSS first (so templ
# components can reference utility classes that exist), then templ.
generate: tailwind
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
	rm -f assets/static/css/app.css
