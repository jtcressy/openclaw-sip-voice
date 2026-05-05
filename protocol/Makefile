SHELL := /bin/sh

.PHONY: test build validate

test: validate
	npm --prefix . test

build: validate

validate:
	node validate.mjs
