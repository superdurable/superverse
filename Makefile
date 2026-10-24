.PHONY: generate-fragments check-fragments compact-coverage-fragments resolve-generated generate generate-proto generate-go generate-web generate-app-template generate-package-readmes \
	generate-studio-readmes build build-web vet compile-tests check check-go check-go-rules check-go-proto \
	check-web check-web-source check-web-build check-generated check-generated-web \
	check-app-template check-app-template-source check-app-template-embedded \
	check-deployment-names check-package-layers check-package-limits check-package-readmes check-go-file-length license-headers check-license-headers \
	check-studio-server-boundary check-mock-scenarios check-rpc-coverage check-flow-registry check-config check-exports check-proto-enums check-test-lanes check-terraform-inputs check-dex-versions check-proto-format check-proto check-generated-proto \
	check-flow-definition format-check check-agent-rules check-prelaunch-identities governance-check \
	test test-integration test-e2e test-claude-live test-example-comparison-live required-lanes overlapping-prs affected-tests affected-specs studio-coverage-map check-local-full-evidence ci \
	lint lint-go lint-web lint-workflows vulnerability-check audit-web \
	install-dexcli install-osv-scanner install-buf

-include infra-as-code/terraform/terraform.mk

GO_PACKAGES = ./cmd/... ./gen/... ./internal/... ./sandbox/coding/cmd/... ./sandbox/coding/internal/...
INTEGRATION_TEST_TIMEOUT ?= 30m
# The tests that only the developer runs, each through its own target, because they cost real-model calls: test-integration
# skips them, and scripts/affected-tests.py reads this line and never suggests them. Names separated by spaces.
DEVELOPER_ONLY_TESTS := TestLiveExampleComparison
EMPTY :=
SPACE := $(EMPTY) $(EMPTY)
export GOCACHE ?= /tmp/superverse-v2-go-cache
export PYTHONDONTWRITEBYTECODE := 1
# Dependency restores, including composed template checks, must not wait on npm advisory services.
export NPM_CONFIG_AUDIT := false
export NPM_CONFIG_FUND := false
# Go commands and the scripts make runs use go.mod alone, whether or not a go.work exists.
export GOWORK := off

# Shared developer tooling; disposable caches default outside the checkout.
TOOL_CACHE ?= /tmp/superverse-v2-tools
DEXCLI_VERSION := v1.15.1
DEXCLI_BINARY ?= $(TOOL_CACHE)/dexcli-$(DEXCLI_VERSION)
OSV_SCANNER_VERSION := v2.5.1
OSV_SCANNER_BINARY ?= $(TOOL_CACHE)/osv-scanner-$(OSV_SCANNER_VERSION)
STATICCHECK_VERSION := v0.7.0
GOLANGCI_LINT_VERSION := v2.12.2
GOVULNCHECK_VERSION := v1.7.0
ACTIONLINT_VERSION := v1.7.12
BUF_VERSION := v1.72.0
BUF_BINARY ?= $(TOOL_CACHE)/buf-$(BUF_VERSION)
BUF := "$(BUF_BINARY)"

# Resolve generated conflicts/drift after merging handwritten sources; pass CONTINUE=1 to resume a rebase.
resolve-generated:
	@python3 scripts/resolve-generated-conflicts.py $(if $(filter 1,$(CONTINUE)),--continue)

# Shared append-only inputs have deterministic assembled views; compact landed observations after rebasing.
generate-fragments:
	@python3 scripts/generated-fragments.py generate

check-fragments:
	@python3 scripts/generated-fragments.py check

compact-coverage-fragments:
	@python3 scripts/generated-fragments.py compact

# Generated sources.
generate: generate-proto generate-go generate-web

# buf.gen.yaml runs the protoc plugins pinned in the go.mod tool block; no system protoc is used.
# The service table of docs/api/resource-grpc.md is generated from the same descriptors.
generate-proto: install-buf
	@$(BUF) generate
	@BUF='$(BUF)' python3 scripts/check-proto-services.py --write
	@npm --prefix web/engine run generate:protobuf

generate-go:
	@go generate ./internal/dexai/agents/session/toolinput
	@go generate ./internal/dexai/agents/session/conversation

generate-web:
	@npm --prefix web/studio run generate:protobuf

# Compose app-template/ (the shared base and each folder of app-template/examples/) into the
# embedded archives and catalog, and render every example's Flow definitions for Studio.
generate-app-template: install-dexcli
	@DEXCLI="$(DEXCLI_BINARY)" go generate ./internal/dexai/app/apptemplate

# Refresh the generated summary block of every internal/ package README.
generate-package-readmes:
	@python3 scripts/check-package-readmes.py --write

# Refresh the generated summary block of every Studio directory README and the Studio module table.
generate-studio-readmes:
	@npm --prefix web/studio run generate:readmes

# Build and static checks. None of these is integration or end-to-end acceptance.
build:
	go build $(GO_PACKAGES)

# Direct-agent command; only local profiles accept the development version.
DEXAI_VERSION ?= 0.0.0-dev
build-dexai-local:
	go build -ldflags "-X main.version=$(DEXAI_VERSION)" -o bin/dexai ./cmd/dexai-cli

build-web:
	@npm --prefix web/studio run build

vet:
	go vet -tags=live $(GO_PACKAGES)

# Compile retained real-dependency tests, including the live model test; run none. `go test -c -o <dir>/`
# names each binary after the last element of its import path, which provider packages share (the three
# kube packages), so the packages compile in rounds of distinct last elements, all rounds at once.
compile-tests:
	@set -eu; compile_directory="$$(mktemp -d)"; \
		trap 'rm -r "$$compile_directory"' EXIT HUP INT TERM; \
		test_packages="$$(go list -tags=live -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' $(GO_PACKAGES))"; \
		test -n "$$test_packages"; \
		rounds="$$(printf '%s\n' $$test_packages | awk -F/ '{ print ++seen[$$NF], $$0 }')"; \
		compiling=""; \
		for round in $$(printf '%s\n' "$$rounds" | cut -d' ' -f1 | sort -un); do \
			mkdir "$$compile_directory/$$round"; \
			go test -tags=live -c -o "$$compile_directory/$$round/" \
				$$(printf '%s\n' "$$rounds" | awk -v round="$$round" '$$1 == round { print $$2 }') & \
			compiling="$$compiling $$!"; \
		done; \
		for process in $$compiling; do wait "$$process"; done

check: check-license-headers check-go check-web

# Every application validation check plus registered Flow graph validation, run locally before a pull request is marked Ready.
ci: check-license-headers governance-check check-dex-versions lint-workflows check-flow-definition check-go check-app-template check-web check-homepage

check-go: build vet compile-tests check-go-rules check-go-proto

check-go-rules: check-studio-server-boundary check-package-layers \
	check-package-limits check-package-readmes check-go-file-length check-mock-scenarios check-rpc-coverage check-flow-registry check-config \
	check-exports check-proto-enums check-test-lanes

check-go-proto: check-proto-format check-generated-proto check-proto

# Studio and Engine generated types, formatting, lint, source rules and production builds (decision 0012).
check-web:
	@npm --prefix web/studio run check
	@npm --prefix web/engine run check

check-web-source:
	@npm --prefix web/studio run check:source

check-web-build:
	@npm --prefix web/studio run check:build

check-homepage:
	@npm --prefix web/homepage ci
	@npm --prefix web/homepage run lint
	@npm --prefix web/homepage test

check-generated:
	@sh scripts/check-generated.sh

check-proto-format: install-buf
	@$(BUF) format -d --exit-code

# Buf STANDARD lint plus the repository's own protocol rules (decision 0011).
check-proto: install-buf
	@$(BUF) lint
	@BUF='$(BUF)' python3 scripts/check-proto-services.py

check-generated-proto: install-buf
	@set -eu; generated_directory="$$(mktemp -d)"; \
		trap 'rm -r "$$generated_directory"' EXIT HUP INT TERM; \
		$(BUF) generate -o "$$generated_directory"; \
		diff -ru "$$generated_directory/gen" gen

check-generated-web:
	@node web/studio/scripts/generate-protobuf-types.mjs --check

# The template's own commit gate plus archive/design drift.
check-app-template:
	@$(MAKE) check-app-template-source
	@$(MAKE) check-app-template-embedded

# Every example of app-template/ is composed into its own directory below
# APP_TEMPLATE_EXAMPLES_DIR, which keeps its restored dependencies and generated
# code between runs, and checked as a Project's source gate checks it. With
# APP_TEMPLATE_CHANGED_PATHS, a file of changed repository paths, only the
# default and the examples those paths reach are checked (the validation workflow),
# and check-app-template-embedded renders the Flow definitions of only those.
APP_TEMPLATE_EXAMPLES_DIR ?= $(TOOL_CACHE)/app-template-examples
APP_TEMPLATE_CHANGED_PATHS ?=
check-app-template-source: install-dexcli
	@set -eu; for example in $$(GOWORK=off go run ./cmd/tools/app-template-archive -list $(if $(APP_TEMPLATE_CHANGED_PATHS),-changed "$(APP_TEMPLATE_CHANGED_PATHS)")); do \
		directory="$(APP_TEMPLATE_EXAMPLES_DIR)/$$example"; \
		GOWORK=off go run ./cmd/tools/app-template-archive -compose "$$example" -into "$$directory"; \
		echo "app-template example $$example: make bootstrap check-static in $$directory"; \
		DEXCLI="$(DEXCLI_BINARY)" $(MAKE) -C "$$directory" bootstrap check-static; \
	done

check-app-template-embedded: install-dexcli
	@APP_TEMPLATE_CHANGED_PATHS="$(APP_TEMPLATE_CHANGED_PATHS)" DEXCLI="$(DEXCLI_BINARY)" sh scripts/check-app-template.sh

check-package-layers:
	@python3 scripts/check-package-layers.py

check-package-limits:
	@python3 scripts/check-package-limits.py

check-package-readmes:
	@python3 scripts/check-package-readmes.py

check-go-file-length:
	@sh scripts/check-go-file-length.sh

check-studio-server-boundary:
	@python3 scripts/check-studio-server-boundary.py

# The mock test model's scenarios against the tool catalog and the application template.
check-mock-scenarios:
	@go run ./cmd/tools/check-mock-scenarios

check-rpc-coverage: check-fragments
	@python3 scripts/rpc-coverage-map.py check

# The Dex SDK registry of every server Flow, built as the server bootstrap builds it.
check-flow-registry:
	@go run ./cmd/tools/check-flow-registry

# Every checked-in server configuration profile, loaded and validated as the server does.
check-config:
	@go run ./cmd/tools/check-config

# The export rules of internal/: no package-internal interface, no needless export, ForTest for test-only exports.
check-exports:
	@go run ./cmd/tools/check-exports

check-proto-enums:
	@go run ./cmd/tools/check-proto-enums

# The Kind addresses of the Dex test lane guard against their owners, and no test builds a Dex client around the guard.
check-test-lanes:
	@python3 scripts/check-test-lanes.py

check-dex-versions:
	@python3 scripts/check-dex-versions.py

.PHONY: build-hosted-tools
build-hosted-tools:
	@docker build --platform linux/arm64 -f internal/dexai/delivery/releases/build/hosted/Dockerfile.tools $$(python3 scripts/check-dex-versions.py --docker-build-args) -t "$${HOSTED_BUILD_TOOLS_TAG:?set HOSTED_BUILD_TOOLS_TAG}" .

# Registered Flow graphs must retain explicit control boundaries and zero diagnostics.
check-flow-definition: install-dexcli
	@python3 scripts/check-flow-definitions.py "$(DEXCLI_BINARY)"

format-check:
	@sh scripts/check-format.sh

check-prelaunch-identities:
	@python3 scripts/check-prelaunch-identities.py

# Relative links and heading anchors of the Markdown documents resolve.
.PHONY: check-doc-links
check-doc-links:
	@python3 scripts/check-doc-links.py

governance-check: check-prelaunch-identities

test: test-integration

# The local lane: Kind for substantive Kubernetes runtime paths, Native otherwise (both for mixed providers).
# scripts/kind-required-paths.txt excludes documentation, tests and configuration-default profiles.
# BASE is the branch it merges into (origin/main by default).
required-lanes:
	@python3 scripts/required-lanes.py $(if $(BASE),--base '$(BASE)')

# Open pull requests that overlap the current change against origin/main.
overlapping-prs:
	@python3 scripts/overlapping-prs.py

# The tests that cover the Go packages the change touches, for the Tests field of the evidence block.
affected-tests:
	@python3 scripts/affected-tests.py $(if $(BASE),--base '$(BASE)')

# The E2E evidence block of pull request PR, as the application validation checks it.
# The Studio E2E specs that run the Studio files the change touches, for the Specs field of the evidence block.
affected-specs:
	@python3 scripts/studio-coverage-map.py affected $(if $(BASE),--base '$(BASE)')

# Refresh the committed Studio coverage map from the spec coverage a Native E2E run recorded (native e2e does it itself).
studio-coverage-map:
	@test -n "$(COVERAGE)" || { echo "usage: make studio-coverage-map COVERAGE=<SUPERVERSE_E2E_COVERAGE_DIR of a spec run>" >&2; exit 2; }
	@python3 scripts/studio-coverage-map.py merge '$(COVERAGE)'

# The DEVELOPER_ONLY_TESTS run only through their own targets.
test-integration:
	@coverage_dir="$$(mktemp -d)"; trap 'rm -rf "$$coverage_dir"' EXIT; \
		SUPERVERSE_TEST_RPC_COVERAGE_FILE="$$coverage_dir/rpc-coverage.json" $(WITH_CLAUDE_KEY) go test -tags=live -count=1 -p 1 -timeout '$(INTEGRATION_TEST_TIMEOUT)' \
			-skip '^($(subst $(SPACE),|,$(strip $(DEVELOPER_ONLY_TESTS))))$$' $(GO_PACKAGES) && \
		python3 scripts/rpc-coverage-map.py merge api-integration "$$coverage_dir/rpc-coverage.json"

test-e2e:
	npm --prefix web/studio run test:e2e

test-claude-live:
	@$(WITH_CLAUDE_KEY) go test -tags=live -count=1 -run '^TestLiveClaude' ./internal/dexai/agents/session/modelprovider

# The real-model comparison of the closest-example start: four Projects on Claude Sonnet 5.5 against the
# running local lane that the SUPERVERSE_TEST_* inputs name, SUPERVERSE_TEST_CODING_IMAGE its coding image. It
# costs model calls, so only the developer runs it, and only through this target (DEVELOPER_ONLY_TESTS).
test-example-comparison-live:
	@$(WITH_CLAUDE_KEY) SUPERVERSE_TEST_EXAMPLE_COMPARISON=real-model go test -tags=live -count=1 -timeout 190m -v \
		-run '^TestLiveExampleComparison$$' ./internal/dexai/api

lint: lint-go lint-web lint-workflows

lint-go:
	@go run honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION) $(GO_PACKAGES)
	@GOLANGCI_LINT_CACHE=$(TOOL_CACHE)/golangci-lint \
		go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run $(GO_PACKAGES)

lint-web:
	@npm --prefix web/studio run format:check
	@npm --prefix web/studio run typecheck
	@npm --prefix web/studio run lint

lint-workflows:
	@go run github.com/rhysd/actionlint/cmd/actionlint@$(ACTIONLINT_VERSION)

vulnerability-check:
	@go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) $(GO_PACKAGES)

audit-web: install-osv-scanner
	@"$(OSV_SCANNER_BINARY)" scan source --lockfile=web/studio/package-lock.json

# Pinned developer tools.
install-dexcli: $(DEXCLI_BINARY)

$(DEXCLI_BINARY):
	@sh scripts/install-pinned-binary.sh dexcli "$(DEXCLI_BINARY)" "$(DEXCLI_VERSION)"

install-osv-scanner: $(OSV_SCANNER_BINARY)

$(OSV_SCANNER_BINARY):
	@sh scripts/install-pinned-binary.sh osv-scanner "$(OSV_SCANNER_BINARY)" "$(OSV_SCANNER_VERSION)"

install-buf: $(BUF_BINARY)

$(BUF_BINARY):
	@sh scripts/install-pinned-binary.sh buf "$(BUF_BINARY)" "$(BUF_VERSION)"

license-headers:
	@python3 scripts/license-headers.py --fix

check-license-headers:
	@python3 scripts/license-headers.py --check
