SHELL := /bin/sh

GO ?= go
GOCACHE ?= /tmp/openclaw-go-build-cache

export GOCACHE

.PHONY: test build test-runtime test-api-fit

test: test-runtime test-api-fit

build: test

test-runtime:
	$(GO) test ./...

test-api-fit:
	cd api-fit && $(GO) test ./...
