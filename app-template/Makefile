SHELL := /usr/bin/env bash
# RUN selects the API tests of make test-app, as go test -run does.
RUN ?=
APPTEST_FLAGS := -tags apptest -count=1 -timeout 5m

.PHONY: bootstrap generate check-fdg-v2 check-static test-app
bootstrap:
	go mod download
	go -C tools/openapi mod download
	npm --prefix web ci
generate:
	./scripts/generate-openapi.sh
check-fdg-v2:
	python3 ./scripts/render-flow-definitions.py
check-static: generate check-fdg-v2
	@test -z "$$(gofmt -l cmd internal)" || { gofmt -d cmd internal; exit 1; }
	go mod tidy -diff
	go vet ./...
	go vet -tags apptest ./...
	go build ./...
	npm --prefix web run build
# The API tests compile against the generated contracts and need the test
# engine: outside a run, superverse-previewctl starts one for go test.
test-app: generate
	@if [ -z "$${SUPERVERSE_APPTEST_RUN:-}" ] && command -v superverse-previewctl >/dev/null; then \
		exec superverse-previewctl apptest run -- go test $(APPTEST_FLAGS) -run '$(RUN)' ./internal/apptest; \
	fi; \
	go test $(APPTEST_FLAGS) -run '$(RUN)' ./internal/apptest
