#!/usr/bin/make -f

BIN := grew
GO := go
PKGS := ./...

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
check:
	$(GO) test -tags devmode -race $(PKGS)

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

.PHONY: distclean
distclean: clean
	rm -rf .codeql-db/ .codeql-results/ .tmpcache/
