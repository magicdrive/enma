# Makefile for enma
#
# Works with GNU make 3.81 (macOS) and later. Releases are published only by
# the release workflow on a v* tag push (docs/development/releasing.md):
# `make preflight tag=vX.Y.Z` checks that the release can run, and
# `make publish tag=vX.Y.Z` runs the tests and that check, then creates and
# pushes the tag.

# Variables
BUILD_DIR = $(CURDIR)/build
CMD_DIRS = $(wildcard cmd/*)
BINARY_NAME = enma
VERSION = $(shell git describe --tags --always)
LDFLAGS += -X "main.version=$(VERSION)"

PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64
GO := GO111MODULE=on CGO_ENABLED=0 go

# staticcheck, pinned (CI runs `make staticcheck` too; needs Go 1.26 or later).
# 2026.2.1 (v0.8.1) cannot read Go 1.27.2's export data ("export data version 5
# is greater than maximum supported version 4"). This development commit can; it
# was verified with Go 1.27.2 (no finding here, findings still reported) and is
# the one used by a sibling project's CI. The release 2026.2.2 (v0.8.2) also
# works with Go 1.27.2: moving to it is a separate decision.
STATICCHECK := honnef.co/go/tools/cmd/staticcheck@v0.7.0-0.dev.0.20261009230814-452d5bb86b45

# Tool versions. CI runs these targets, so the versions here are the ones CI
# uses; Renovate updates them through the `# renovate:` comments.
# renovate: datasource=go depName=github.com/magicdrive/hephaestus
HEPHAESTUS_VERSION ?= v1.1.0
HEPHAESTUS ?= go run github.com/magicdrive/hephaestus@$(HEPHAESTUS_VERSION)
# renovate: datasource=go depName=golang.org/x/vuln
GOVULNCHECK_VERSION ?= v1.8.0
# renovate: datasource=go depName=github.com/rhysd/actionlint
ACTIONLINT_VERSION ?= v1.7.12
# renovate: datasource=npm depName=renovate
RENOVATE_VERSION ?= 44.149.3

# Tag of the local, never published build used by the Homebrew end-to-end
# test (`make homebrew-e2e`).
E2E_TAG ?= v99.0.0

# Argments
tag =

# Default target
.PHONY: all
all: help

# Build all artifacts
.PHONY: build
build: clean
	@mkdir -p $(BUILD_DIR)
	@$(GO) build -o $(BUILD_DIR)/$(BINARY_NAME) -ldflags "$(LDFLAGS)"
	@chmod 755 $(BUILD_DIR)/$(BINARY_NAME)

# Build artifacts for all platforms and release
.PHONY: release-build
release-build: clean $(PLATFORMS)
	@echo "Release files are created in the $(BUILD_DIR) directory."

# Build each platform (windows binaries get the .exe suffix)
os = $(word 1,$(subst /, ,$@))
arch = $(word 2,$(subst /, ,$@))
platform_bin = $(BUILD_DIR)/$(os)-$(arch)/$(BINARY_NAME)$(if $(filter windows,$(os)),.exe)
$(PLATFORMS):
	@mkdir -p $(BUILD_DIR)
	GOOS=$(os) GOARCH=$(arch) $(GO) build -o $(platform_bin) -ldflags "$(LDFLAGS)" .
	chmod 755 $(platform_bin)

# Run go test for each directory
.PHONY: test
test:
	@$(GO) test $(CURDIR)/...

# Run go test with verbose output and clear test cache
.PHONY: test-verbose
test-verbose:
	@$(GO) clean -testcache
	@$(GO) test -v $(CURDIR)/...

# Run go test with the race detector (needs cgo)
.PHONY: test-race
test-race:
	@GO111MODULE=on CGO_ENABLED=1 go test -count=1 -race $(CURDIR)/...

# Run the pinned staticcheck
.PHONY: staticcheck
staticcheck:
	@$(GO) run $(STATICCHECK) ./...

# Format the Go sources
.PHONY: fmt
fmt:
	@gofmt -w .

# Fail when a Go source is not gofmt-formatted
.PHONY: fmt-check
fmt-check:
	@unformatted="$$(gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then echo "gofmt needed (run make fmt):"; echo "$$unformatted"; exit 1; fi

# Run go vet for the host and for Windows
.PHONY: vet
vet:
	@$(GO) vet ./...
	@GOOS=windows $(GO) vet ./...

# Fail when go.mod or go.sum is not tidy
.PHONY: tidy-check
tidy-check:
	@$(GO) mod tidy -diff

# Static analysis (the pinned staticcheck)
.PHONY: lint
lint: staticcheck

# Known vulnerabilities in the dependencies and the Go standard library
.PHONY: vuln
vuln:
	@$(GO) run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

# Check install.sh and the scripts (shellcheck must be installed)
.PHONY: shellcheck
shellcheck:
	@sh -n install.sh
	@shellcheck -s sh install.sh scripts/g404-report.sh scripts/verify-dist.sh scripts/release-toolchain.sh
	@shellcheck scripts/homebrew-e2e.sh

# Build, then run the binary: --version and --help
.PHONY: smoke
smoke: build
	@$(BUILD_DIR)/$(BINARY_NAME) --version | grep -q '^enma version .' || { echo "smoke: unexpected --version output"; exit 1; }
	@$(BUILD_DIR)/$(BINARY_NAME) --help | grep -q '^Usage: enma ' || { echo "smoke: unexpected --help output"; exit 1; }
	@echo "smoke: ok ($$($(BUILD_DIR)/$(BINARY_NAME) --version))"

# Every local quality gate that CI runs on each pull request (the test jobs
# also need zsh, fish, wget and shellcheck; see ci.yml)
.PHONY: check
check: fmt-check vet tidy-check lint test-race vuln smoke hephaestus-check

# Lint the GitHub Actions workflows
.PHONY: actionlint
actionlint:
	@$(GO) run github.com/rhysd/actionlint/cmd/actionlint@$(ACTIONLINT_VERSION)

# Validate renovate.json
.PHONY: renovate-check
renovate-check:
	@npx --yes --package renovate@$(RENOVATE_VERSION) -- renovate-config-validator --strict renovate.json

# Validate hephaestus.yaml
.PHONY: hephaestus-check
hephaestus-check:
	@$(HEPHAESTUS) check

# Build every release archive locally, as the release workflow does, without
# a tag (version 0.0.0-SNAPSHOT) and without publishing anything; then check
# the archives and install one of them with install.sh
.PHONY: snapshot
snapshot:
	@$(HEPHAESTUS) run --snapshot --stages build,archive,checksum --fresh
	@sh scripts/verify-dist.sh dist 0.0.0-SNAPSHOT
	@ENMA_INSTALLER_DIST=$(CURDIR)/dist $(GO) test -count=1 -run '^TestDistArchives$$' ./internal/installer/

# Build the archives and the Homebrew formula for E2E_TAG locally (nothing is
# published), then install the formula with the real Homebrew in a
# throw-away tap. Installs into the local Homebrew prefix and removes
# everything on exit; refuses to run when enma is installed with Homebrew.
.PHONY: homebrew-e2e
homebrew-e2e:
	@$(HEPHAESTUS) run --stages build,archive,checksum,formula --tag $(E2E_TAG) --fresh
	@scripts/homebrew-e2e.sh dist

# Release safety checks before a tag is pushed (needs a GitHub token in
# GITHUB_TOKEN or GH_TOKEN, e.g. GH_TOKEN=$$(gh auth token)):
#   make preflight tag=vX.Y.Z
# --strict also fails on WARN and UNKNOWN. `make publish` runs it without
# --strict (PUBLISH_PREFLIGHT_FLAGS): a FAIL or UNKNOWN still stops the tag,
# a WARN (for example no tag ruleset) is printed and does not.
PREFLIGHT_FLAGS ?= --strict
PUBLISH_PREFLIGHT_FLAGS ?=
.PHONY: preflight
preflight:
	@if [ -z "$(tag)" ]; then echo "usage: make preflight tag=vX.Y.Z"; exit 2; fi
	@$(HEPHAESTUS) preflight --tag $(tag) $(PREFLIGHT_FLAGS)

# Everything that can be checked locally before a release: the quality gates,
# the release archives, and the release preflight
.PHONY: release-check
release-check: check snapshot preflight

# Install application. Use `go install`
.PHONY: install
install:
	@echo "Installing enma..."
	@$(GO) install -ldflags "$(LDFLAGS)"

# Clean build artifacts
.PHONY: clean
clean:
	@rm -rf $(BUILD_DIR)

# Install dev tools
.PHONY: dev-tools
dev-tools:
	go install "github.com/magicdrive/goreg@latest"
	go install "github.com/magicdrive/kirke@latest"

# Execute goreg -w to entire gofile.
.PHONY: goreg
goreg:
	git ls-files | grep -e '.go$$' | xargs -I GOFILE goreg -w GOFILE

# Publish to github.com: push the tag; the release workflow builds and
# publishes the release (docs/development/releasing.md). The release
# preflight runs first and stops the target when a check fails.
.PHONY: publish
publish: test-verbose
	@if [ -z "$(tag)" ]; then \
		echo "Error: version is not set. Please set it and try again."; \
		echo "ex) make publish tag=v0.0.1"; \
		echo ; \
		echo ; \
		exit 1; \
		fi
	@$(MAKE) --no-print-directory preflight tag=$(tag) PREFLIGHT_FLAGS="$(PUBLISH_PREFLIGHT_FLAGS)"
	git tag $(tag)
	git push origin $(tag)


# Show help
.PHONY: help
help:
	@echo "Makefile commands:"
	@echo "  make build             - Build all artifacts"
	@echo "  make release-build     - Build artifacts for multiple platforms with version info"
	@echo "  make install           - Install application with go install"
	@echo "  make test              - Run go test"
	@echo "  make test-verbose      - Run go test -v with go clean -testcache"
	@echo "  make test-race         - Run go test -race"
	@echo "  make staticcheck       - Run the pinned staticcheck"
	@echo "  make lint              - Static analysis (staticcheck)"
	@echo "  make fmt               - Format the Go sources"
	@echo "  make fmt-check         - Fail when a Go source is not formatted"
	@echo "  make vet               - Run go vet (host and windows)"
	@echo "  make tidy-check        - Fail when go.mod or go.sum is not tidy"
	@echo "  make vuln              - Run govulncheck"
	@echo "  make shellcheck        - Check install.sh and the scripts"
	@echo "  make smoke             - Build and run --version and --help"
	@echo "  make check             - Every local quality gate of CI"
	@echo "  make actionlint        - Lint the GitHub Actions workflows"
	@echo "  make renovate-check    - Validate renovate.json"
	@echo "  make hephaestus-check  - Validate hephaestus.yaml"
	@echo "  make snapshot          - Build the release archives locally (nothing is published)"
	@echo "  make homebrew-e2e      - Install the generated formula with the local Homebrew"
	@echo "  make preflight tag=<tag> - Release safety checks before tagging"
	@echo "  make release-check tag=<tag> - check, snapshot and preflight"
	@echo "  make clean             - Remove build artifacts"
	@echo "  make dev-tools         - Install dev tools"
	@echo "  make goreg             - Execute goreg -w to entire gofile"
	@echo "  make publish tag=<tag> - Preflight, then push the tag (the release workflow publishes)"
	@echo "  make help              - Show this message"

