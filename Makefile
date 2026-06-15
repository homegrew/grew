#!/usr/bin/make -f

BIN := grew
GO := go
# UNIT_PKGS excludes the tests/ directory
UNIT_PKGS := $(shell $(GO) list ./... | grep -v /tests)
# INTEGRATION_PKGS is just the tests directory
INTEGRATION_PKGS := ./tests/...

.PHONY: all
all: build check

.PHONY: build
build:
	$(GO) build -o $(BIN)

.PHONY: dev
dev:
	$(GO) build -tags devmode -o $(BIN)

.PHONY: install
install:
	$(GO) install

.PHONY: check
check: test-unit

.PHONY: check-all
check-all: test-unit test-integration test-smoke test-e2e

.PHONY: test-unit
test-unit:
	$(GO) test -tags devmode -race $(UNIT_PKGS)

.PHONY: test-unit-coverage
test-unit-coverage:
	$(GO) test -tags devmode -race -coverprofile=coverage_unit.txt -covermode=atomic $(UNIT_PKGS)

.PHONY: test-integration
test-integration:
	$(GO) test ./tests/integration

.PHONY: test-smoke
test-smoke:
	$(GO) test -v ./tests/smoke

.PHONY: test-e2e
test-e2e:
	$(GO) test -v ./tests/e2e -run TestLiveEndToEnd

.PHONY: test-integration-coverage
test-integration-coverage:
	@mkdir -p coverage/raw
	@rm -rf coverage/raw/*
	GOCOVERDIR=$(shell pwd)/coverage/raw $(GO) test -tags "devmode" -cover -coverpkg=./... -count=1 ./tests/integration

	$(GO) tool covdata textfmt -i=coverage/raw -o coverage_integration.txt
	@rm -rf coverage/raw
	@echo "Integration coverage report generated at coverage_integration.txt"

.PHONY: lint
lint:
	golangci-lint run

.PHONY: fmt
fmt:
	$(GO) fmt ./...

.PHONY: mod-tidy
mod-tidy:
	$(GO) mod tidy

.PHONY: clean
clean:
	rm -f $(BIN) grew_test_bin
	rm -f coverage*.txt coverage.out
	rm -rf .cache
	rm -rf coverage/

.PHONY: build-binary
build-binary:
	$(GO) build -o grew -trimpath -ldflags "-s -w -X main.Version=$(VERSION)"

.PHONY: build-fat-binary
build-fat-binary:
	GOOS=darwin GOARCH=amd64 $(GO) build -o grew_amd64 -trimpath -ldflags "-s -w -X main.Version=$(VERSION)"
	GOOS=darwin GOARCH=arm64 $(GO) build -o grew_arm64 -trimpath -ldflags "-s -w -X main.Version=$(VERSION)"
	lipo -create -output grew_universal grew_amd64 grew_arm64

.PHONY: distclean
distclean: clean
	rm -rf .codeql-db/ .codeql-results/ .tmpcache/ \
		grew grew_arm64 grew_amd64 grew_universal
