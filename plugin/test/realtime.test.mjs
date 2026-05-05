import { Buffer } from "node:buffer";
import test from "node:test";
import assert from "node:assert/strict";

import {
  REALTIME_VOICE_AUDIO_FORMAT_G711_ULAW_8KHZ,
} from "openclaw/plugin-sdk/realtime-voice";
import {
  createSipVoiceRealtimeBridgeSession,
  resolveSipVoiceRealtimeAudioFormat,
  resolveSipVoiceRealtimeProvider,
} from "../src/realtime.ts";

const DEFAULT_MODEL = "gemini-3.1-flash-live-preview";

function createSipVoiceConfig(overrides = {}) {
  return {
    enabled: true,
    bridge: {
      url: "ws://127.0.0.1:9077",
    },
    maxConcurrentCalls: 1,
    audio: {
      format: "g711-ulaw-8khz",
    },
    realtime: {
      provider: "google",
      model: DEFAULT_MODEL,
      toolPolicy: "safe-read-only",
      providers: {
        google: {
          apiKeyRef: "op://voice/google/api-key",
          voice: "Aoede",
        },
      },
      instructions: "Keep answers short.",
      introMessage: "Greet the caller once.",
    },
    ...overrides,
  };
}

function createRealtimeProviderFake(id, options = {}) {
  const calls = {
    resolveConfig: [],
    isConfigured: [],
    createBridge: [],
  };
  const bridge = {
    acknowledgedMarkCount: 0,
    greetedWith: [],
    connected: false,
    connect: async () => {
      bridge.connected = true;
    },
    sendAudio: () => {},
    setMediaTimestamp: () => {},
    sendUserMessage: () => {},
    triggerGreeting: (instructions) => {
      bridge.greetedWith.push(instructions);
    },
    handleBargeIn: () => {},
    submitToolResult: () => {},
    acknowledgeMark: () => {
      bridge.acknowledgedMarkCount += 1;
    },
    close: () => {
      bridge.connected = false;
    },
    isConnected: () => bridge.connected,
  };
  const provider = {
    id,
    label: `Fake ${id}`,
    autoSelectOrder: options.autoSelectOrder,
    resolveConfig: (ctx) => {
      calls.resolveConfig.push(ctx);
      return options.resolveConfig?.(ctx) ?? ctx.rawConfig;
    },
    isConfigured: (ctx) => {
      calls.isConfigured.push(ctx);
      return options.isConfigured?.(ctx) ?? true;
    },
    createBridge: (req) => {
      calls.createBridge.push(req);
      return bridge;
    },
  };

  return {
    provider,
    calls,
    bridge,
  };
}

test("resolveSipVoiceRealtimeAudioFormat maps g711-ulaw-8khz to the SDK ulaw 8k format", () => {
  const config = createSipVoiceConfig();

  assert.equal(
    resolveSipVoiceRealtimeAudioFormat(config),
    REALTIME_VOICE_AUDIO_FORMAT_G711_ULAW_8KHZ,
  );
});

test("resolveSipVoiceRealtimeProvider passes provider id, configs, full config, and default model", () => {
  const config = createSipVoiceConfig();
  const fullConfig = {
    runtime: {
      mode: "test",
    },
  };
  const google = createRealtimeProviderFake("google", {
    autoSelectOrder: 10,
    resolveConfig: ({ rawConfig }) => ({
      ...rawConfig,
      resolved: true,
    }),
  });
  const autoSelectableButWrong = createRealtimeProviderFake("openai", {
    autoSelectOrder: 1,
  });

  const resolved = resolveSipVoiceRealtimeProvider({
    config,
    fullConfig,
    providers: [autoSelectableButWrong.provider, google.provider],
  });

  assert.equal(resolved.provider, google.provider);
  assert.equal(autoSelectableButWrong.calls.resolveConfig.length, 0);
  assert.equal(autoSelectableButWrong.calls.isConfigured.length, 0);
  assert.equal(google.calls.resolveConfig.length, 1);
  assert.equal(google.calls.isConfigured.length, 1);
  assert.equal(google.calls.resolveConfig[0].cfg, fullConfig);
  assert.equal(google.calls.isConfigured[0].cfg, fullConfig);
  assert.deepEqual(google.calls.resolveConfig[0].rawConfig, {
    apiKeyRef: "op://voice/google/api-key",
    voice: "Aoede",
    model: DEFAULT_MODEL,
  });
  assert.deepEqual(resolved.providerConfig, {
    apiKeyRef: "op://voice/google/api-key",
    voice: "Aoede",
    model: DEFAULT_MODEL,
    resolved: true,
  });
});

test("createSipVoiceRealtimeBridgeSession uses immediate mark ack, intro greeting, and provided audio sink", () => {
  const config = createSipVoiceConfig();
  const fullConfig = {
    runtime: {
      mode: "test",
    },
  };
  const google = createRealtimeProviderFake("google");
  const receivedAudio = [];
  const receivedMarks = [];
  const providedAudioSink = {
    isOpen: () => true,
    sendAudio: (audio) => {
      receivedAudio.push(audio);
    },
    sendMark: (markName) => {
      receivedMarks.push(markName);
    },
  };
  const sessionAudioSink = {
    sendAudio: () => {
      throw new Error("session audioSink override should not be used");
    },
  };

  createSipVoiceRealtimeBridgeSession({
    config,
    fullConfig,
    audioSink: providedAudioSink,
    providers: [google.provider],
    session: {
      audioSink: sessionAudioSink,
      initialGreetingInstructions: "Wrong greeting.",
      markStrategy: "transport",
      triggerGreetingOnReady: true,
    },
  });

  assert.equal(google.calls.createBridge.length, 1);
  assert.equal(
    google.calls.createBridge[0].providerConfig,
    google.calls.isConfigured[0].providerConfig,
  );
  assert.deepEqual(
    google.calls.createBridge[0].audioFormat,
    REALTIME_VOICE_AUDIO_FORMAT_G711_ULAW_8KHZ,
  );
  assert.equal(google.calls.createBridge[0].instructions, "Keep answers short.");

  const audio = Buffer.from([1, 2, 3]);
  google.calls.createBridge[0].onAudio(audio);
  assert.deepEqual(receivedAudio, [audio]);

  google.calls.createBridge[0].onMark?.("mark-1");
  assert.equal(google.bridge.acknowledgedMarkCount, 1);
  assert.deepEqual(receivedMarks, []);

  google.calls.createBridge[0].onReady();
  assert.deepEqual(google.bridge.greetedWith, ["Greet the caller once."]);
});
