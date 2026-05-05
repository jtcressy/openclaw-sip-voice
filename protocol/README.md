# OpenClaw SIP Voice Local Bridge Protocol

This directory defines the local protocol between the OpenClaw SIP Voice plugin and the SIP UA/media bridge. The proof-of-concept transport is WebSocket over loopback at `ws://127.0.0.1:9077`.

The protocol intentionally exposes call control and canonical audio frames only. Plugin-visible messages must not include SIP, SDP, RTP, UniFi credentials, Gemini/OpenAI configuration, tokens, passwords, or other secrets.

## Versioning

Current protocol version: `1.0`

Every message carries `protocolVersion`. Receivers must reject any message whose version is not exactly `1.0`.

Deterministic failure behavior for an unsupported version:

1. Treat the received message as invalid before processing its `type`.
2. Emit an `error` event using the current local protocol version `1.0`.
3. Set `error.code` to `unsupported_protocol_version`.
4. Include `error.expectedProtocolVersions: ["1.0"]`.
5. Set `fatal: true`, then close the WebSocket with close code `1002`.

## Transport

- POC URL: `ws://127.0.0.1:9077`
- Encoding: UTF-8 JSON text frames
- Bridge-to-plugin events: `hello`, `status`, `call.start`, `audio.in`, `call.end`, `error`
- Plugin-to-bridge commands: `status.get`, `call.answer`, `call.dial`, `audio.out`, `audio.clear`, `call.hangup`

## Audio

Canonical audio frames are:

- `format`: `g711_ulaw`
- `sampleRateHz`: `8000`
- `channels`: `1`
- `payloadEncoding`: `base64`
- `frameDurationMs`: `20`

For `audio.in` and `audio.out`, the `audio.payload` field contains one 20 ms g711 u-law frame, encoded as base64. The decoded payload length is 160 bytes.

## Message Shape

All messages use a small envelope:

- `protocolVersion`: currently `1.0`
- `type`: message discriminator
- `sentAt`: ISO-8601 timestamp
- Commands also include `commandId`
- Call-scoped messages include `callId`

Call identifiers, command identifiers, and remote handles are opaque at this boundary. Do not send SIP URIs, SDP blobs, RTP header data, credential names, API keys, tokens, or provider configuration through this protocol.

## Files

- `schemas/message.schema.json`: JSON Schema for all protocol messages.
- `types/messages.ts`: TypeScript contract types.
- `fixtures/valid/*.json`: valid protocol examples.
- `fixtures/invalid/*.json`: invalid examples that enforce boundary constraints.
- `validator.mjs`: no-dependency validator used by tests and the validation command.
- `test/protocol-fixtures.test.mjs`: Node test coverage for fixtures and version failure behavior.

## Validation

From the repository root:

```sh
node protocol/validate.mjs
```

Or run the Node test suite:

```sh
npm --prefix protocol test
```
