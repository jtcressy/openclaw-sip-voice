# Bridge Image

`Dockerfile.bridge` builds the real bridge service image from the repo root context.

The builder stage compiles `bridge/cmd/openclaw-sip-voice-bridge` with `CGO_ENABLED=0`. The runtime stage is `scratch`, runs as UID/GID `65532`, exposes the SIP/WebSocket/health ports, and uses:

```text
ENTRYPOINT ["/openclaw-sip-voice-bridge"]
```

Bridge configuration remains environment-only; SIP credentials must not be passed as build arguments or copied into the image.
