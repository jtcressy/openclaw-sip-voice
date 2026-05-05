# Plugin ImageVolume Payload

`Dockerfile.plugin` builds the plugin artifact from the repo root context and copies the packaged plugin root to `/` in the final image.

Expected final image content:

```text
/package.json
/openclaw.plugin.json
/tsconfig.json
/index.ts
/src/
/node_modules/typebox/
```

The image is an ImageVolume filesystem artifact. It must not define `ENTRYPOINT` or `CMD`; Kubernetes mounts the image root at the OpenClaw plugin path.
