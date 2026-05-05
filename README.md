# OpenClaw SIP Voice

Root packaging for the OpenClaw SIP Voice plugin artifact and bridge image.

## Images

- `ghcr.io/jtcressy/openclaw-sip-voice-plugin`
- `ghcr.io/jtcressy/openclaw-sip-voice-bridge`

The plugin image is a filesystem artifact for Kubernetes ImageVolume use. It is not a service image and must not grow an entrypoint or command.

Deployment manifests must consume digest references only:

```text
ghcr.io/jtcressy/openclaw-sip-voice-plugin@sha256:<digest>
ghcr.io/jtcressy/openclaw-sip-voice-bridge@sha256:<digest>
```

Do not put POC status in image names, tags, repository names, Kubernetes object names, or package names.

## Local Commands

```sh
make test
make build
make images
make test-plugin
make test-bridge
make test-protocol
```

Root hooks delegate to `plugin/Makefile`, `bridge/Makefile`, and `protocol/Makefile` when those lane-owned Makefiles exist. Until then, missing hooks are skipped by default. Set `STRICT_OWNER_HOOKS=1` to make missing lane hooks fail.

`make images` builds local images using these defaults:

```text
PLUGIN_IMAGE=ghcr.io/jtcressy/openclaw-sip-voice-plugin
BRIDGE_IMAGE=ghcr.io/jtcressy/openclaw-sip-voice-bridge
IMAGE_TAG=local
PLATFORMS=linux/amd64
```

## Packaging Contracts

- `Dockerfile.plugin` builds the `artifact` target from `scratch` using the current Node builder image. The final image root is the OpenClaw plugin root and contains `package.json`, `openclaw.plugin.json`, `tsconfig.json`, `index.ts`, `src/`, and production runtime dependencies under `node_modules/`. It has no entrypoint or command.
- `Dockerfile.bridge` builds the `runtime` target with the current Go builder image by compiling `bridge/cmd/openclaw-sip-voice-bridge` with `CGO_ENABLED=0`, then copies the binary into a `scratch` runtime image with `ENTRYPOINT ["/openclaw-sip-voice-bridge"]`.
- `make images` and `.github/workflows/images.yml` build both images from the repo root context so Dockerfiles can copy the real plugin and bridge sources.
- `.github/workflows/images.yml` records digest-form promotion refs in the workflow summary. Publishing to GHCR happens on `main`/tag pushes, or manually via `workflow_dispatch` with `push=true`.
- Root workflows use GitHub-hosted `ubuntu-24.04` runners. Pinned actions using Node 24 require Actions Runner `v2.327.1` or newer on any future self-hosted runner.

The plugin artifact intentionally keeps OpenClaw host imports as peer imports. The image packages the plugin-owned runtime dependency needed for path loading (`typebox`) but does not vendor the OpenClaw host SDK.

`make test` includes lightweight packaging checks that assert the plugin image remains an ImageVolume artifact, both image contexts stay at repo root, the bridge image has a real entrypoint, and Dockerfiles copy/build the expected runtime content.

## Ownership Boundaries

Packaging owns the root image Dockerfiles, root `Makefile`, `.dockerignore`, README, `packaging/**`, and workflow skeletons.

The `plugin/`, `bridge/`, and `protocol/` directories are lane-owned. Root tooling may call their Makefile hooks but must not implement their internals.
