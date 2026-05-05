# OpenClaw SIP Voice Bridge

Go runtime foundation for the local OpenClaw SIP Voice bridge.

The bridge loads configuration only from environment variables, constructs the
SIP UA/media stack from the `sipgo`/`diago` API-fit evidence, exposes a local
WebSocket protocol endpoint, and serves process health on HTTP. UniFi Talk SIP
registration stays inert when credentials are absent; when all credential fields
are present, the bridge builds a `diago.RegisterTransaction`, sends REGISTER,
keeps the registration refreshed with `QualifyLoop`, and attempts Unregister on
shutdown.

## Run

```sh
go run ./cmd/openclaw-sip-voice-bridge
```

Default endpoints:

- WebSocket protocol: `ws://127.0.0.1:9077`
- Liveness: `http://127.0.0.1:9078/healthz`
- Readiness: `http://127.0.0.1:9078/readyz`
- Metrics foundation: `http://127.0.0.1:9078/metrics`

## Environment

All configuration is environment-only:

| Variable | Default | Notes |
| --- | --- | --- |
| `UNIFI_TALK_SIP_SERVER` | unset | Redacted in logs/status. Required with the other UniFi SIP fields to enable registration. |
| `UNIFI_TALK_SIP_USERNAME` | unset | Redacted in logs/status. |
| `UNIFI_TALK_SIP_PASSWORD` | unset | Redacted in logs/status. |
| `UNIFI_TALK_SIP_EXTENSION` | unset | Redacted in logs/status. |
| `SIP_TRANSPORT` | `udp` | UDP is the validated runtime path from API-fit. |
| `SIP_BIND_ADDR` | `0.0.0.0:5060` | Local SIP bind IP and port. |
| `SIP_ADVERTISE_ADDR` | `127.0.0.1:5060` | Contact/SDP advertised IP and port. Use the macvlan IP here. |
| `RTP_PORT_MIN` | `10000` | Parsed and validated for future media binding. |
| `RTP_PORT_MAX` | `10019` | Parsed and validated for future media binding. |
| `BRIDGE_WS_ADDR` | `127.0.0.1:9077` | WebSocket listen address. Protocol v1.0 still advertises the default URL. |
| `METRICS_ADDR` | `127.0.0.1:9078` | Health and metrics listen address. |
| `CODECS` | `g711_ulaw` | POC is PCMU-only. PCMA/transcoding is deferred until the end-to-end voice path is validated. |

The four UniFi SIP values must be provided together. Missing credentials keep
the bridge alive in a degraded, unregistered state without exposing any SIP
credential fields through the plugin protocol.

Registration errors are reduced to safe status reason codes such as
`auth_failed`, `registrar_unavailable`, and `registration_failed`. Raw SIP
errors and credential values are not emitted through logs, health, or protocol
status.

`/healthz` is process liveness and stays HTTP 200 for degraded-but-running
states. `/readyz` is call readiness and only returns HTTP 200 when the bridge
state is ready and SIP registration is registered.

## Test

```sh
make test
```

This runs both the real bridge module tests and the isolated `api-fit` module.
