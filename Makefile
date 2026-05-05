SHELL := /bin/sh

PLUGIN_IMAGE ?= ghcr.io/jtcressy/openclaw-sip-voice-plugin
BRIDGE_IMAGE ?= ghcr.io/jtcressy/openclaw-sip-voice-bridge
IMAGE_TAG ?= local
PLATFORMS ?= linux/amd64

DOCKER ?= docker
IMAGE_OUTPUT ?= type=docker
IMAGE_BUILD_FLAGS ?=

PLUGIN_DOCKERFILE ?= Dockerfile.plugin
BRIDGE_DOCKERFILE ?= Dockerfile.bridge
PLUGIN_CONTEXT ?= .
BRIDGE_CONTEXT ?= .
PLUGIN_IMAGE_TARGET ?= artifact
BRIDGE_IMAGE_TARGET ?= runtime

STRICT_OWNER_HOOKS ?= 0

.PHONY: help test build images image-plugin image-bridge test-plugin test-bridge test-protocol build-plugin build-bridge build-protocol verify-packaging verify-image-packaging image-refs

help:
	@printf '%s\n' "OpenClaw SIP Voice packaging targets:"
	@printf '%s\n' "  make test            Run root packaging checks and lane test hooks"
	@printf '%s\n' "  make build           Run lane build hooks when present"
	@printf '%s\n' "  make images          Build plugin and bridge images locally"
	@printf '%s\n' "  make test-plugin     Delegate to plugin/Makefile test when present"
	@printf '%s\n' "  make test-bridge     Delegate to bridge/Makefile test when present"
	@printf '%s\n' "  make test-protocol   Delegate to protocol/Makefile test when present"

test: verify-packaging test-plugin test-bridge test-protocol

build: build-plugin build-bridge build-protocol

images: image-plugin image-bridge

image-plugin:
	$(DOCKER) buildx build --platform $(PLATFORMS) --file $(PLUGIN_DOCKERFILE) --target $(PLUGIN_IMAGE_TARGET) --tag $(PLUGIN_IMAGE):$(IMAGE_TAG) --output $(IMAGE_OUTPUT) $(IMAGE_BUILD_FLAGS) $(PLUGIN_CONTEXT)

image-bridge:
	$(DOCKER) buildx build --platform $(PLATFORMS) --file $(BRIDGE_DOCKERFILE) --target $(BRIDGE_IMAGE_TARGET) --tag $(BRIDGE_IMAGE):$(IMAGE_TAG) --output $(IMAGE_OUTPUT) $(IMAGE_BUILD_FLAGS) $(BRIDGE_CONTEXT)

image-refs:
	@printf '%s\n' "$(PLUGIN_IMAGE):$(IMAGE_TAG)"
	@printf '%s\n' "$(BRIDGE_IMAGE):$(IMAGE_TAG)"
	@printf '%s\n' "Deployment manifests must use image@sha256:digest refs emitted by CI or registry inspection."

verify-packaging:
	@case "$(PLUGIN_IMAGE) $(BRIDGE_IMAGE)" in *poc*|*POC*) echo "POC status must not be encoded in image names" >&2; exit 1;; *) :;; esac
	@if grep -E '^(ENTRYPOINT|CMD)[[:space:]]' $(PLUGIN_DOCKERFILE) >/dev/null; then echo "$(PLUGIN_DOCKERFILE) must remain an ImageVolume filesystem artifact, not a service image" >&2; exit 1; fi
	@if [ "$(PLUGIN_CONTEXT)" != "." ]; then echo "PLUGIN_CONTEXT must be repo root for real plugin packaging" >&2; exit 1; fi
	@if [ "$(BRIDGE_CONTEXT)" != "." ]; then echo "BRIDGE_CONTEXT must be repo root for real bridge packaging" >&2; exit 1; fi
	@if [ "$(PLUGIN_IMAGE_TARGET)" != "artifact" ]; then echo "PLUGIN_IMAGE_TARGET must be artifact" >&2; exit 1; fi
	@if [ "$(BRIDGE_IMAGE_TARGET)" != "runtime" ]; then echo "BRIDGE_IMAGE_TARGET must be runtime" >&2; exit 1; fi
	@$(MAKE) --no-print-directory verify-image-packaging
	@printf '%s\n' "packaging checks passed"

verify-image-packaging:
	@grep -q 'COPY plugin/package.json ./package.json' $(PLUGIN_DOCKERFILE) || { echo "$(PLUGIN_DOCKERFILE) must copy plugin package metadata" >&2; exit 1; }
	@grep -q 'COPY plugin/openclaw.plugin.json ./openclaw.plugin.json' $(PLUGIN_DOCKERFILE) || { echo "$(PLUGIN_DOCKERFILE) must copy openclaw.plugin.json" >&2; exit 1; }
	@grep -q 'node_modules/typebox' $(PLUGIN_DOCKERFILE) || { echo "$(PLUGIN_DOCKERFILE) must verify packaged runtime dependency typebox" >&2; exit 1; }
	@grep -q 'COPY bridge/go.mod bridge/go.sum' $(BRIDGE_DOCKERFILE) || { echo "$(BRIDGE_DOCKERFILE) must build from bridge module metadata" >&2; exit 1; }
	@grep -q 'go build .*./cmd/openclaw-sip-voice-bridge' $(BRIDGE_DOCKERFILE) || { echo "$(BRIDGE_DOCKERFILE) must compile the bridge binary" >&2; exit 1; }
	@grep -q '^ENTRYPOINT \["/openclaw-sip-voice-bridge"\]' $(BRIDGE_DOCKERFILE) || { echo "$(BRIDGE_DOCKERFILE) must define the bridge entrypoint" >&2; exit 1; }

test-plugin:
	@if [ -f plugin/Makefile ]; then \
		$(MAKE) -C plugin test; \
	elif [ "$(STRICT_OWNER_HOOKS)" = "1" ]; then \
		echo "plugin/Makefile test hook is missing" >&2; exit 1; \
	else \
		echo "test-plugin: plugin owner hook not present; skipped"; \
	fi

test-bridge:
	@if [ -f bridge/Makefile ]; then \
		$(MAKE) -C bridge test; \
	elif [ "$(STRICT_OWNER_HOOKS)" = "1" ]; then \
		echo "bridge/Makefile test hook is missing" >&2; exit 1; \
	else \
		echo "test-bridge: bridge owner hook not present; skipped"; \
	fi

test-protocol:
	@if [ -f protocol/Makefile ]; then \
		$(MAKE) -C protocol test; \
	elif [ "$(STRICT_OWNER_HOOKS)" = "1" ]; then \
		echo "protocol/Makefile test hook is missing" >&2; exit 1; \
	else \
		echo "test-protocol: protocol owner hook not present; skipped"; \
	fi

build-plugin:
	@if [ -f plugin/Makefile ]; then \
		$(MAKE) -C plugin build; \
	elif [ "$(STRICT_OWNER_HOOKS)" = "1" ]; then \
		echo "plugin/Makefile build hook is missing" >&2; exit 1; \
	else \
		echo "build-plugin: plugin owner hook not present; skipped"; \
	fi

build-bridge:
	@if [ -f bridge/Makefile ]; then \
		$(MAKE) -C bridge build; \
	elif [ "$(STRICT_OWNER_HOOKS)" = "1" ]; then \
		echo "bridge/Makefile build hook is missing" >&2; exit 1; \
	else \
		echo "build-bridge: bridge owner hook not present; skipped"; \
	fi

build-protocol:
	@if [ -f protocol/Makefile ]; then \
		$(MAKE) -C protocol build; \
	elif [ "$(STRICT_OWNER_HOOKS)" = "1" ]; then \
		echo "protocol/Makefile build hook is missing" >&2; exit 1; \
	else \
		echo "build-protocol: protocol owner hook not present; skipped"; \
	fi
