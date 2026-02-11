# Binaries
CLI_BINARY=ups-cli
MONITOR_BINARY=ups-monitor

# Directories
CLI_DIR=./cmd/cli
MONITOR_DIR=./cmd/monitor

.PHONY: all cli monitor clean test check deb

# Default target
all: cli monitor

# Build the Debian package (requires nfpm)
deb: all
	nfpm package --packager deb --target .

# Build the main CLI tool
cli:
	go build -o $(CLI_BINARY) $(CLI_DIR)

# Build the monitor service
monitor:
	go build -o $(MONITOR_BINARY) $(MONITOR_DIR)

# Run tests
test:
	go test ./...

# Basic lint/vet check
check:
	go vet ./...

# Remove binaries
clean:
	rm -f $(CLI_BINARY) $(MONITOR_BINARY) *.deb
