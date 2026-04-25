#!/usr/bin/make -f

BIN := grew
GO := go
PKGS := ./...

.PHONY: all
all: build check

.PHONY: build
build: generate
	$(GO) build -o $(BIN)

.PHONY: dev
dev: generate
	$(GO) build -tags devmode -o $(BIN)

.PHONY: install
install: generate
	$(GO) install

.PHONY: check
check: generate
	$(GO) test -tags devmode -race $(PKGS)

.PHONY: generate
generate:
	$(GO) generate ./internal/...

.PHONY: lint
lint:
	golangci-lint run

.PHONY: fmt
fmt:
	$(GO) fmt $(PKGS)

.PHONY: mod-tidy
mod-tidy:
	$(GO) mod tidy

.PHONY: clean
clean:
	rm -f $(BIN) grew_test_bin
	rm -f coverage.out
	rm -rf .cache
	echo "v0.0.0-UNKNOWN" > $(CURDIR)/internal/version/version.txt

.PHONY: distclean
distclean: clean
	rm -rf .codeql-db/ .codeql-results/ .tmpcache/
