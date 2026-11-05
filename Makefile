GO      ?= go
BIN     ?= bin/norm
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo devel)

# Native Tree-sitter grammars are compiled into the executable. A native C
# compiler is required; setting GOOS/GOARCH alone is not cross-compilation.
export CGO_ENABLED := 1
.DEFAULT_GOAL := help

.PHONY: package
package: ## Package the native target (requires GoReleaser v2, Python, and a C compiler)
	@config=$$(mktemp); trap 'rm -f "$$config"' EXIT; \
	  python3 scripts/native-release-config.py "$$config" && \
	  NORM_LINK_FLAGS="$$(case $$($(GO) env GOHOSTOS) in linux|windows) printf '%s' '-linkmode external -extldflags -static';; esac)" \
	  goreleaser release --snapshot --clean --skip=publish,announce --config="$$config"

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-z-]+:.*?## ' $(MAKEFILE_LIST) | \
	  awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build the binary into bin/
	$(GO) build -ldflags "-X main.version=$(VERSION)" -o $(BIN) ./cmd/norm

.PHONY: test
test: ## Run the tests with the race detector
	$(GO) test -race ./...

.PHONY: lint
lint: ## Run golangci-lint
	golangci-lint run

.PHONY: fmt
fmt: ## Format the source
	gofmt -w .

.PHONY: dogfood
dogfood: build ## Measure this repository with the binary just built
	$(BIN) --all --axes complexity --fail-on headline

.PHONY: check
check: ## Everything CI runs
	@gofmt -l . | grep . && { echo "not gofmt'd"; exit 1; } || true
	$(GO) vet ./...
	$(GO) test -race ./...
	$(MAKE) dogfood

.PHONY: clean
clean: ## Remove build output and caches
	rm -rf bin/ .norm/

.PHONY: skill
skill: ## Regenerate the in-repo agent skill from agent/norm.md
	@./install.sh --skill-only --agent claude

.PHONY: skill-check
skill-check: ## Fail if the generated skill has drifted from agent/norm.md
	@cp .claude/skills/norm/SKILL.md /tmp/norm-skill-before
	@./install.sh --skill-only --agent claude >/dev/null
	@diff -q /tmp/norm-skill-before .claude/skills/norm/SKILL.md >/dev/null || { \
	  echo "the checked-in skill is out of date; run: make skill"; exit 1; }
	@echo "skill is in sync with agent/norm.md"
