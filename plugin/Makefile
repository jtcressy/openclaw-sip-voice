SHELL := /bin/sh

.PHONY: test build check-json check-side-effects test-bridge-client test-realtime test-runtime test-public-surfaces

test: check-json check-side-effects test-bridge-client test-realtime test-runtime test-public-surfaces

build: test

check-json:
	node -e 'for (const file of ["package.json","openclaw.plugin.json","tsconfig.json"]) JSON.parse(require("node:fs").readFileSync(file, "utf8"));'

check-side-effects:
	node scripts/check-side-effects.mjs

test-bridge-client:
	node test/bridge-client.test.mjs

test-realtime:
	node --import ./test/openclaw-sdk-fake-loader.mjs test/realtime.test.mjs

test-runtime:
	node --test test/runtime.test.mjs

test-public-surfaces:
	node --test test/public-surfaces.test.mjs
