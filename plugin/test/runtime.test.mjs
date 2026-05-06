import assert from "node:assert/strict";
import { registerHooks } from "node:module";
import test from "node:test";

const pluginEntryFakeUrl = `data:text/javascript,${encodeURIComponent(`
export function buildJsonPluginConfigSchema() {
  return {
    safeParse(value) {
      return { success: true, data: value ?? {} };
    },
  };
}
`)}`;

const realtimeFakeUrl = `data:text/javascript,${encodeURIComponent(`
export const REALTIME_VOICE_AUDIO_FORMAT_G711_ULAW_8KHZ = {
  encoding: "g711_ulaw",
  sampleRateHz: 8000,
  channels: 1,
};

export function resolveConfiguredRealtimeVoiceProvider(params) {
  const provider = params.providers?.find(
    (candidate) => candidate.id === params.configuredProviderId,
  );
  if (!provider) {
    throw new Error(params.noRegisteredProviderMessage ?? "No realtime provider registered");
  }
  return {
    provider,
    providerConfig: {
      ...(params.providerConfigs?.[params.configuredProviderId] ?? {}),
      model: params.defaultModel,
    },
  };
}

export function createRealtimeVoiceBridgeSession() {
  return {
    connect() {},
    sendAudio() {},
    speak() {},
    close() {},
  };
}
`)}`;

registerHooks({
  resolve(specifier, context, nextResolve) {
    if (specifier === "openclaw/plugin-sdk/plugin-entry") {
      return { shortCircuit: true, url: pluginEntryFakeUrl };
    }
    if (specifier === "openclaw/plugin-sdk/realtime-voice") {
      return { shortCircuit: true, url: realtimeFakeUrl };
    }
    if (specifier.startsWith(".") && specifier.endsWith(".js")) {
      return nextResolve(`${specifier.slice(0, -3)}.ts`, context);
    }
    return nextResolve(specifier, context);
  },
});

const { createSipVoiceRuntime } = await import("../src/runtime.ts");

const audioFormat = {
  format: "g711_ulaw",
  sampleRateHz: 8000,
  channels: 1,
  frameDurationMs: 20,
  payloadEncoding: "base64",
};

class FakeBridgeClient {
  constructor() {
    this.commands = [];
    this.connectCalls = 0;
    this.disconnectCalls = [];
    this.eventListeners = new Set();
    this.stateListeners = new Set();
    this.nextCommandId = 1;
    this.state = {
      connectionState: "disconnected",
      lastError: null,
      lastStatus: null,
      hello: null,
    };
  }

  getState() {
    return this.state;
  }

  onEvent(listener) {
    this.eventListeners.add(listener);
    return () => {
      this.eventListeners.delete(listener);
    };
  }

  onStateChange(listener) {
    this.stateListeners.add(listener);
    return () => {
      this.stateListeners.delete(listener);
    };
  }

  async connect() {
    this.connectCalls += 1;
    this.setState({ connectionState: "connected", lastError: null });
  }

  disconnect(code, reason) {
    this.disconnectCalls.push({ code, reason });
    this.setState({ connectionState: "disconnected" });
  }

  sendStatusGet() {
    return this.command("status.get");
  }

  sendCallAnswer(options) {
    return this.command("call.answer", options);
  }

  sendCallDial(options) {
    return this.command("call.dial", {
      ...options,
      audio: options.audio ?? audioFormat,
    });
  }

  sendAudioOut(options) {
    const audio =
      options.audio ??
      ({
        ...audioFormat,
        payload: Buffer.from(options.payload).toString("base64"),
      });
    return this.command("audio.out", {
      callId: options.callId,
      sequence: options.sequence,
      audio,
    });
  }

  sendAudioClear(options) {
    return this.command("audio.clear", {
      callId: options.callId,
      scope: "queued",
      ...(options.reason === undefined ? {} : { reason: options.reason }),
    });
  }

  sendCallHangup(options) {
    return this.command("call.hangup", options);
  }

  emit(event) {
    if (event.type === "hello") {
      this.setState({ hello: event });
    }
    if (event.type === "status") {
      this.setState({ lastStatus: event });
    }
    if (event.type === "error") {
      this.setState({ lastError: new Error(event.error.message) });
    }
    for (const listener of this.eventListeners) {
      listener(event);
    }
  }

  setState(patch) {
    this.state = { ...this.state, ...patch };
    for (const listener of this.stateListeners) {
      listener(this.state);
    }
  }

  command(type, fields = {}) {
    const command = {
      protocolVersion: "1.0",
      type,
      sentAt: "2026-05-05T12:00:00.000Z",
      commandId: `cmd_fake${String(this.nextCommandId).padStart(6, "0")}`,
      ...fields,
    };
    this.nextCommandId += 1;
    this.commands.push(command);
    return command;
  }

  commandsOf(type) {
    return this.commands.filter((command) => command.type === type);
  }
}

class FakeRealtimeSession {
  constructor(params, options = {}) {
    this.params = params;
    this.connectError = options.connectError;
    this.connectCalls = 0;
    this.sentAudio = [];
    this.spoken = [];
    this.closeCalls = 0;
  }

  connect() {
    this.connectCalls += 1;
    if (this.connectError) {
      return Promise.reject(this.connectError);
    }
  }

  sendAudio(audio) {
    this.sentAudio.push(Buffer.from(audio));
  }

  speak(message) {
    this.spoken.push(message);
  }

  close() {
    this.closeCalls += 1;
  }
}

function createRealtimeFactory(options = {}) {
  const sessions = [];
  return {
    sessions,
    create(params) {
      const session = new FakeRealtimeSession(params, options);
      sessions.push(session);
      return session;
    },
  };
}

function makeConfig(overrides = {}) {
  return {
    enabled: true,
    bridge: { url: "ws://127.0.0.1:9077", ...(overrides.bridge ?? {}) },
    maxConcurrentCalls: overrides.maxConcurrentCalls,
    audio: { format: "g711-ulaw-8khz", ...(overrides.audio ?? {}) },
    realtime: {
      provider: "fake",
      model: "test-live-model",
      toolPolicy: "safe-read-only",
      providers: {},
      instructions: "answer briefly",
      introMessage: "hello",
      ...(overrides.realtime ?? {}),
    },
  };
}

function makeParams({
  bridge = new FakeBridgeClient(),
  realtime = createRealtimeFactory(),
  config = makeConfig({ maxConcurrentCalls: 1 }),
  fullConfig = {},
} = {}) {
  return {
    bridge,
    realtime,
    params: {
      config,
      fullConfig,
      runtime: {},
      logger: {},
      bridgeClient: bridge,
      createRealtimeSession: realtime.create.bind(realtime),
      realtimeProviders: [{ id: "fake" }],
    },
  };
}

function callStart(overrides = {}) {
  return {
    protocolVersion: "1.0",
    type: "call.start",
    sentAt: "2026-05-05T12:00:01.000Z",
    callId: "call_abcdef",
    direction: "inbound",
    state: "ringing",
    remote: { handle: "+15551234567", displayName: "Caller" },
    audio: audioFormat,
    ...overrides,
  };
}

function audioIn(callId, payload, sequence = 0) {
  return {
    protocolVersion: "1.0",
    type: "audio.in",
    sentAt: "2026-05-05T12:00:02.000Z",
    callId,
    sequence,
    timestampMs: sequence * 20,
    audio: {
      ...audioFormat,
      payload: Buffer.from(payload).toString("base64"),
    },
  };
}

function callEnd(callId, overrides = {}) {
  return {
    protocolVersion: "1.0",
    type: "call.end",
    sentAt: "2026-05-05T12:00:03.000Z",
    callId,
    outcome: "completed",
    durationMs: 1000,
    ...overrides,
  };
}

function bridgeError(overrides = {}) {
  return {
    protocolVersion: "1.0",
    type: "error",
    sentAt: "2026-05-05T12:00:04.000Z",
    fatal: false,
    error: {
      code: "call_rejected",
      message: "Call rejected.",
      retryable: false,
      ...overrides,
    },
  };
}

test("inbound call lifecycle answers, bridges audio, clears output, and closes", async () => {
  const { bridge, realtime, params } = makeParams();
  const runtime = await createSipVoiceRuntime(params);

  assert.equal(bridge.connectCalls, 1);
  assert.equal(bridge.commandsOf("status.get").length, 1);

  bridge.emit(callStart());

  assert.equal(bridge.commandsOf("call.answer").length, 1);
  assert.equal(realtime.sessions.length, 1);
  assert.equal(realtime.sessions[0].connectCalls, 1);
  assert.equal(runtime.getStatus().activeCalls, 1);

  const inboundPayload = Buffer.alloc(160, 7);
  bridge.emit(audioIn("call_abcdef", inboundPayload));
  assert.deepEqual(realtime.sessions[0].sentAudio, [inboundPayload]);

  const outboundPayload = Buffer.alloc(160, 8);
  realtime.sessions[0].params.audioSink.sendAudio(outboundPayload);
  assert.equal(bridge.commandsOf("audio.out").length, 1);
  assert.equal(bridge.commandsOf("audio.out")[0].sequence, 0);
  assert.equal(bridge.commandsOf("audio.out")[0].audio.payload, outboundPayload.toString("base64"));

  realtime.sessions[0].params.audioSink.clear("barge_in");
  assert.equal(bridge.commandsOf("audio.clear").at(-1).reason, "barge_in");

  bridge.emit(callEnd("call_abcdef"));

  assert.equal(realtime.sessions[0].closeCalls, 1);
  assert.equal(runtime.getStatus().activeCalls, 0);
  assert.equal(runtime.tail().events.at(-1).type, "call.end");
  assert.deepEqual(runtime.tail(0), { events: [], errors: [] });
});

test("inbound call connect failure closes realtime session and hangs up", async () => {
  const realtime = createRealtimeFactory({ connectError: new Error("connect failed") });
  const { bridge, params } = makeParams({ realtime });
  const runtime = await createSipVoiceRuntime(params);

  bridge.emit(callStart());
  await new Promise((resolve) => setImmediate(resolve));

  assert.equal(realtime.sessions.length, 1);
  assert.equal(realtime.sessions[0].connectCalls, 1);
  assert.equal(realtime.sessions[0].closeCalls, 1);
  assert.equal(bridge.commandsOf("call.hangup").length, 1);
  assert.equal(bridge.commandsOf("call.hangup")[0].callId, "call_abcdef");
  assert.equal(runtime.getStatus().calls[0].state, "ending");
  assert.equal(runtime.getStatus().recentErrors.at(-1).code, "session_connect_failed");
});

test("audio sink frames variable realtime chunks into canonical bridge frames", async () => {
  const { bridge, realtime, params } = makeParams();
  await createSipVoiceRuntime(params);

  bridge.emit(callStart());
  const sink = realtime.sessions[0].params.audioSink;

  const first = Buffer.alloc(160, 1);
  const second = Buffer.alloc(160, 2);
  const partialA = Buffer.alloc(80, 3);
  sink.sendAudio(Buffer.concat([first, second, partialA]));

  let commands = bridge.commandsOf("audio.out");
  assert.equal(commands.length, 2);
  assert.equal(commands[0].sequence, 0);
  assert.equal(commands[0].audio.payload, first.toString("base64"));
  assert.equal(commands[1].sequence, 1);
  assert.equal(commands[1].audio.payload, second.toString("base64"));

  const partialB = Buffer.alloc(80, 4);
  sink.sendAudio(partialB);
  commands = bridge.commandsOf("audio.out");
  assert.equal(commands.length, 3);
  assert.equal(commands[2].sequence, 2);
  assert.equal(commands[2].audio.payload, Buffer.concat([partialA, partialB]).toString("base64"));

  const partialC = Buffer.alloc(80, 5);
  sink.sendAudio(partialC);
  assert.equal(bridge.commandsOf("audio.out").length, 3);

  sink.clear("barge_in");
  const partialD = Buffer.alloc(80, 6);
  sink.sendAudio(partialD);
  assert.equal(bridge.commandsOf("audio.out").length, 3);

  sink.sendAudio(partialD);
  commands = bridge.commandsOf("audio.out");
  assert.equal(commands.length, 4);
  assert.equal(commands[3].sequence, 3);
  assert.equal(commands[3].audio.payload, Buffer.concat([partialD, partialD]).toString("base64"));
});

test("maxConcurrentCalls defaults to one and rejects a second inbound call", async () => {
  const config = makeConfig();
  delete config.maxConcurrentCalls;
  const { bridge, realtime, params } = makeParams({ config });
  const runtime = await createSipVoiceRuntime(params);

  bridge.emit(callStart({ callId: "call_first1" }));
  bridge.emit(callStart({ callId: "call_second2", remote: { handle: "+15557654321" } }));

  assert.equal(runtime.getStatus().maxConcurrentCalls, 1);
  assert.equal(runtime.getStatus().activeCalls, 1);
  assert.equal(realtime.sessions.length, 1);
  assert.equal(bridge.commandsOf("call.answer").length, 1);
  assert.equal(bridge.commandsOf("call.hangup").length, 1);
  assert.equal(bridge.commandsOf("call.hangup")[0].callId, "call_second2");
  assert.match(runtime.getStatus().recentErrors.at(-1).message, /maxConcurrentCalls/);
});

test("outbound call reserves capacity, passes model through, speaks, and hangs up", async () => {
  const config = makeConfig({
    maxConcurrentCalls: 1,
    realtime: { model: "model-from-config" },
  });
  const { bridge, realtime, params } = makeParams({ config });
  const runtime = await createSipVoiceRuntime(params);

  const dial = await runtime.call({ handle: "+15550001111", displayName: "Target" }, "opening line");
  assert.equal(bridge.commandsOf("call.dial").length, 1);
  assert.equal(runtime.getStatus().pendingCalls, 1);
  await assert.rejects(() => runtime.call("+15550002222"), /maxConcurrentCalls/);

  bridge.emit(
    callStart({
      callId: "call_out001",
      direction: "outbound",
      state: "active",
      remote: { handle: "+15550001111", displayName: "Target" },
      requestedByCommandId: dial.commandId,
    }),
  );

  assert.equal(runtime.getStatus().pendingCalls, 0);
  assert.equal(realtime.sessions[0].params.config.realtime.model, "model-from-config");
  assert.deepEqual(realtime.sessions[0].spoken, ["opening line"]);
  assert.equal(runtime.getStatus().realtime.model, "model-from-config");

  await runtime.speak("call_out001", "follow up");
  assert.deepEqual(realtime.sessions[0].spoken, ["opening line", "follow up"]);

  await runtime.hangup("call_out001");
  assert.equal(bridge.commandsOf("call.hangup").at(-1).reason, "user_request");
  assert.equal(realtime.sessions[0].closeCalls, 1);
});

test("bridge command error releases pending outbound call capacity", async () => {
  const { bridge, params } = makeParams();
  const runtime = await createSipVoiceRuntime(params);

  const dial = await runtime.call("+15550001111");
  assert.equal(runtime.getStatus().pendingCalls, 1);

  bridge.emit(bridgeError({ commandId: dial.commandId }));

  assert.equal(runtime.getStatus().pendingCalls, 0);
  assert.equal(runtime.getStatus().recentErrors.at(-1).code, "call_rejected");
  await assert.doesNotReject(() => runtime.call("+15550002222"));
});

test("status and tail do not expose realtime provider configs or full config secrets", async () => {
  const config = makeConfig({
    maxConcurrentCalls: 1,
    realtime: {
      providers: {
        fake: {
          apiKey: "sip-provider-secret",
          password: "sip-provider-password",
        },
      },
    },
  });
  const { params } = makeParams({
    config,
    fullConfig: {
      sipPassword: "full-config-sip-secret",
      nested: { token: "full-config-token" },
    },
  });
  const runtime = await createSipVoiceRuntime(params);

  const serialized = JSON.stringify({
    status: runtime.getStatus(),
    tail: runtime.tail(),
  });

  assert.equal(runtime.getStatus().config.realtime.providerConfigCount, 1);
  assert.doesNotMatch(serialized, /sip-provider-secret/);
  assert.doesNotMatch(serialized, /sip-provider-password/);
  assert.doesNotMatch(serialized, /full-config-sip-secret/);
  assert.doesNotMatch(serialized, /full-config-token/);
  assert.doesNotMatch(serialized, /apiKey/);
  assert.doesNotMatch(serialized, /sipPassword/);
});
