import assert from "node:assert/strict";
import { readdir, readFile } from "node:fs/promises";
import { registerHooks } from "node:module";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

registerHooks({
  resolve(specifier, context, nextResolve) {
    if (specifier.startsWith(".") && specifier.endsWith(".js")) {
      return nextResolve(`${specifier.slice(0, -3)}.ts`, context);
    }
    return nextResolve(specifier, context);
  },
});

const { SipVoiceBridgeClient } = await import("../src/bridge-client.ts");
const {
  BRIDGE_EVENT_TYPES,
  ProtocolValidationError,
  UNSUPPORTED_VERSION_CLOSE_CODE,
  parseBridgeEvent,
  validateBridgeEvent,
  validateProtocolMessage,
} = await import("../src/protocol.ts");

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const validFixtureDir = path.join(repoRoot, "protocol", "fixtures", "valid");
const invalidFixtureDir = path.join(repoRoot, "protocol", "fixtures", "invalid");

class FakeTransport {
  readyState = 0;
  sent = [];
  closes = [];
  listeners = new Map();

  addEventListener(type, listener) {
    const listeners = this.listeners.get(type) ?? new Set();
    listeners.add(listener);
    this.listeners.set(type, listeners);
  }

  removeEventListener(type, listener) {
    this.listeners.get(type)?.delete(listener);
  }

  send(data) {
    this.sent.push(data);
  }

  close(code, reason) {
    this.closes.push({ code, reason });
    this.readyState = 3;
    this.emit("close", { code, reason });
  }

  open() {
    this.readyState = 1;
    this.emit("open", {});
  }

  message(data) {
    this.emit("message", { data });
  }

  emit(type, event) {
    for (const listener of this.listeners.get(type) ?? []) {
      listener(event);
    }
  }
}

test("protocol parser accepts all valid bridge event fixtures", async () => {
  const files = await readdir(validFixtureDir);
  const bridgeEventFiles = [];

  for (const file of files) {
    const fixture = await readFixture("valid", file);
    if (!BRIDGE_EVENT_TYPES.includes(fixture.type)) {
      continue;
    }

    bridgeEventFiles.push(file);
    assert.deepEqual(parseBridgeEvent(JSON.stringify(fixture)), fixture);
  }

  assert.deepEqual(bridgeEventFiles.sort(), [
    "audio-in.valid.json",
    "call-end.completed.valid.json",
    "call-end.error.valid.json",
    "call-start.inbound.valid.json",
    "call-start.outbound.valid.json",
    "error.valid.json",
    "hello.valid.json",
    "status.registered.valid.json",
    "status.unregistered.valid.json",
  ]);
});

test("protocol validator rejects invalid protocol fixtures", async () => {
  const files = await readdir(invalidFixtureDir);

  for (const file of files) {
    const fixture = await readFixture("invalid", file);
    const result = validateProtocolMessage(fixture);
    assert.equal(result.ok, false, `${file} should fail validation`);
  }
});

test("event validation rejects required boundary failures", async () => {
  const unsupportedVersion = validateBridgeEvent(
    await readFixture("invalid", "hello.unknown-protocol-version.invalid.json"),
  );
  assert.equal(unsupportedVersion.ok, false);
  assert.equal(unsupportedVersion.error.code, "unsupported_protocol_version");
  assert.equal(unsupportedVersion.error.closeCode, UNSUPPORTED_VERSION_CLOSE_CODE);

  const unknownType = await readFixture("valid", "status.registered.valid.json");
  unknownType.type = "status.nope";
  assertErrorAt(validateBridgeEvent(unknownType), "$.type", "unknown message type");

  const missingCallId = validateBridgeEvent(
    await readFixture("invalid", "audio-in.missing-call-id.invalid.json"),
  );
  assertErrorAt(missingCallId, "$.callId", "is required");

  const wrongAudioLength = await readFixture("valid", "audio-in.valid.json");
  wrongAudioLength.audio.payload = Buffer.alloc(161).toString("base64");
  assertErrorAt(validateBridgeEvent(wrongAudioLength), "$.audio.payload", "160 bytes");

  const forbiddenField = validateBridgeEvent(
    await readFixture("invalid", "status.unexpected-sip-credential-field.invalid.json"),
  );
  assertErrorAt(forbiddenField, "$.sipCredentialRef", "not allowed");
});

test("client tracks connection, hello, status, and bridge events", async () => {
  const { client, transport } = createConnectedClient();
  const events = [];
  client.onEvent((event) => events.push(event.type));

  await openClient(client, transport);

  const hello = await readFixture("valid", "hello.valid.json");
  const status = await readFixture("valid", "status.registered.valid.json");

  transport.message(JSON.stringify(hello));
  transport.message(JSON.stringify(status));

  assert.equal(client.getState().connectionState, "connected");
  assert.deepEqual(client.getState().hello, hello);
  assert.deepEqual(client.getState().lastStatus, status);
  assert.deepEqual(events, ["hello", "status"]);
});

test("client closes the transport on unsupported protocol versions", async () => {
  const { client, transport } = createConnectedClient();
  await openClient(client, transport);

  transport.message(
    JSON.stringify(await readFixture("invalid", "hello.unknown-protocol-version.invalid.json")),
  );

  assert.equal(client.getState().connectionState, "disconnected");
  assert.equal(client.getState().lastError instanceof ProtocolValidationError, true);
  assert.equal(client.getState().lastError.code, "unsupported_protocol_version");
  assert.deepEqual(transport.closes, [
    {
      code: UNSUPPORTED_VERSION_CLOSE_CODE,
      reason: "unsupported protocol version",
    },
  ]);
});

test("client sends command fixtures over the injected transport", async () => {
  const { client, transport } = createConnectedClient();
  await openClient(client, transport);

  const statusGet = await readFixture("valid", "status-get.valid.json");
  await assertSendsFixture(transport, statusGet, () =>
    client.sendStatusGet({
      commandId: statusGet.commandId,
      sentAt: statusGet.sentAt,
    }),
  );

  const answer = await readFixture("valid", "call-answer.valid.json");
  await assertSendsFixture(transport, answer, () =>
    client.sendCallAnswer({
      commandId: answer.commandId,
      sentAt: answer.sentAt,
      callId: answer.callId,
    }),
  );

  const dial = await readFixture("valid", "call-dial.valid.json");
  await assertSendsFixture(transport, dial, () =>
    client.sendCallDial({
      commandId: dial.commandId,
      sentAt: dial.sentAt,
      remote: dial.remote,
      audio: dial.audio,
    }),
  );

  const audioOut = await readFixture("valid", "audio-out.valid.json");
  await assertSendsFixture(transport, audioOut, () =>
    client.sendAudioOut({
      commandId: audioOut.commandId,
      sentAt: audioOut.sentAt,
      callId: audioOut.callId,
      sequence: audioOut.sequence,
      audio: audioOut.audio,
    }),
  );

  const clear = await readFixture("valid", "audio-clear.valid.json");
  await assertSendsFixture(transport, clear, () =>
    client.sendAudioClear({
      commandId: clear.commandId,
      sentAt: clear.sentAt,
      callId: clear.callId,
      reason: clear.reason,
    }),
  );

  const hangup = await readFixture("valid", "call-hangup.valid.json");
  await assertSendsFixture(transport, hangup, () =>
    client.sendCallHangup({
      commandId: hangup.commandId,
      sentAt: hangup.sentAt,
      callId: hangup.callId,
      reason: hangup.reason,
    }),
  );
});

function createConnectedClient() {
  const transport = new FakeTransport();
  const client = new SipVoiceBridgeClient({
    transportFactory: () => transport,
    commandIdFactory: () => "cmd_test_000001",
    now: () => new Date("2026-05-05T17:00:00.000Z"),
  });
  return { client, transport };
}

async function openClient(client, transport) {
  const pending = client.connect();
  assert.equal(client.getState().connectionState, "connecting");
  transport.open();
  await pending;
  assert.equal(client.getState().connectionState, "connected");
}

async function assertSendsFixture(transport, expected, send) {
  const sentBefore = transport.sent.length;
  const command = send();
  assert.deepEqual(command, expected);
  assert.deepEqual(JSON.parse(transport.sent.at(-1)), expected);
  assert.equal(transport.sent.length, sentBefore + 1);
}

function assertErrorAt(result, pathLabel, messagePart) {
  assert.equal(result.ok, false);
  assert.equal(
    result.errors.some(
      (error) => error.path === pathLabel && error.message.includes(messagePart),
    ),
    true,
    `expected ${pathLabel} to include ${JSON.stringify(messagePart)} in ${JSON.stringify(result.errors)}`,
  );
}

async function readFixture(kind, name) {
  const dir = kind === "valid" ? validFixtureDir : invalidFixtureDir;
  return JSON.parse(await readFile(path.join(dir, name), "utf8"));
}
