# cyberpower — thin aliases over `vulcan` (github.com/grooverlabs/vulcan;
# per-repo constants in vulcan.yaml), the target names of llm/BUILD.md
# §"Makefile". No database and no dev loop, so no db-*, migrate-* or dev
# targets. Two binaries (ups-monitor, ups-cli) in one .deb, cyberpower-ups.
# Cross-compile for the Pi with ARCH=arm64; the version comes from git describe.
.PHONY: build test package generate doctor lint deps test-cover clean help

ARCH ?= $(shell go env GOARCH)

build:      ; vulcan build --arch $(ARCH)
test:       ; vulcan test
package:    ; vulcan package --arch $(ARCH)
generate:   ; vulcan generate
doctor:     ; vulcan doctor
lint:       ; golangci-lint run ./...
deps:       ; go mod tidy
test-cover: ; vulcan generate && go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out | tail -1
clean:      ; rm -rf dist coverage.out assets/static/css/app.css && find . -name '*_templ.go' -delete
help:       ; @grep -oE '^[a-z][a-z0-9 -]*:' Makefile | tr -d ':' | tr ' ' '\n' | sort -u | column -c 100
