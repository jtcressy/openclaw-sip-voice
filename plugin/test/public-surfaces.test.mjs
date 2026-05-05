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

export function definePluginEntry(entry) {
  return entry;
}
`)}`;

const typeboxFakeUrl = `data:text/javascript,${encodeURIComponent(`
function schema(type, options) {
  return { type, ...(options ?? {}) };
}

export const Type = {
  Object(properties, options) {
    return { type: "object", properties, ...(options ?? {}) };
  },
  String(options) {
    return schema("string", options);
  },
  Integer(options) {
    return schema("integer", options);
  },
  Optional(child) {
    return { ...child, optional: true };
  },
  Literal(value, options) {
    return { const: value, ...(options ?? {}) };
  },
  Union(anyOf, options) {
    return { anyOf, ...(options ?? {}) };
  },
};
`)}`;

const runtimeFakeUrl = `data:text/javascript,${encodeURIComponent(`
export async function createSipVoiceRuntime(params) {
  if (typeof globalThis.__sipVoiceRuntimeFactory !== "function") {
    throw new Error("test runtime factory was not installed");
  }
  return globalThis.__sipVoiceRuntimeFactory(params);
}
`)}`;

registerHooks({
  resolve(specifier, context, nextResolve) {
    if (specifier === "openclaw/plugin-sdk/plugin-entry") {
      return { shortCircuit: true, url: pluginEntryFakeUrl };
    }
    if (specifier === "typebox") {
      return { shortCircuit: true, url: typeboxFakeUrl };
    }
    if (
      specifier === "./src/runtime.js" &&
      context.parentURL?.endsWith("/index.ts")
    ) {
      return { shortCircuit: true, url: runtimeFakeUrl };
    }
    if (specifier.startsWith(".") && specifier.endsWith(".js")) {
      return nextResolve(`${specifier.slice(0, -3)}.ts`, context);
    }
    return nextResolve(specifier, context);
  },
});

const pluginEntry = (await import("../index.ts")).default;

function makeConfig(overrides = {}) {
  return {
    enabled: true,
    bridge: { url: "ws://127.0.0.1:9077", ...(overrides.bridge ?? {}) },
    maxConcurrentCalls: 1,
    audio: { format: "g711-ulaw-8khz" },
    realtime: {
      provider: "fake",
      model: "test-live-model",
      toolPolicy: "safe-read-only",
      providers: {},
      ...(overrides.realtime ?? {}),
    },
    ...overrides,
  };
}

function makeApi({ mode = "full", pluginConfig = makeConfig() } = {}) {
  const api = {
    pluginConfig,
    config: {},
    runtime: {},
    logger: {},
    registrationMode: mode,
    tools: [],
    gatewayMethods: new Map(),
    cliRegistrations: [],
    services: [],
    registerTool(tool) {
      this.tools.push(tool);
    },
    registerGatewayMethod(name, handler) {
      this.gatewayMethods.set(name, handler);
    },
    registerCli(callback, metadata) {
      this.cliRegistrations.push({ callback, metadata });
    },
    registerService(service) {
      this.services.push(service);
    },
  };
  return api;
}

function makeRuntime() {
  const calls = [];
  return {
    calls,
    getStatus() {
      calls.push({ method: "getStatus" });
      return {
        enabled: true,
        bridge: { url: "ws://127.0.0.1:9077?token=token-secret" },
        sipPassword: "provider-secret",
        recentErrors: [{ message: "authorization=BearerSecret" }],
      };
    },
    tail(limit) {
      calls.push({ method: "tail", limit });
      return {
        events: [{ type: "bridge.status", secretToken: "tail-secret" }],
        errors: [],
      };
    },
    async call(target, message) {
      calls.push({ method: "call", target, message });
      return {
        commandId: "cmd_fake000001",
        remote: typeof target === "string" ? { handle: target } : target,
        messageQueued: message !== undefined,
        token: "call-secret",
      };
    },
    async speak(callId, message) {
      calls.push({ method: "speak", callId, message });
      return { callId, messageQueued: true, apiKey: "speak-secret" };
    },
    async hangup(callId) {
      calls.push({ method: "hangup", callId });
      return {
        protocolVersion: "1.0",
        type: "call.hangup",
        callId,
        reason: "user_request",
        password: "hangup-secret",
      };
    },
    async stop() {
      calls.push({ method: "stop" });
    },
  };
}

function installRuntimeFactory(runtime = makeRuntime()) {
  const createCalls = [];
  globalThis.__sipVoiceRuntimeFactory = async (params) => {
    createCalls.push(params);
    return runtime;
  };
  return { runtime, createCalls };
}

function registerFullPlugin(runtime = makeRuntime()) {
  const installed = installRuntimeFactory(runtime);
  const api = makeApi();
  pluginEntry.register(api);
  return { api, ...installed };
}

function parseToolText(result) {
  assert.equal(result.content[0].type, "text");
  assert.deepEqual(JSON.parse(result.content[0].text), result.details);
  return result.details;
}

async function invokeGateway(api, method, params) {
  const handler = api.gatewayMethods.get(method);
  assert.equal(typeof handler, "function", `${method} was not registered`);
  let response;
  await handler({
    params,
    respond(ok, payload) {
      response = { ok, payload };
    },
  });
  assert.ok(response, `${method} did not respond`);
  return response;
}

class FakeCommand {
  constructor(signature) {
    this.signature = signature;
    this.children = [];
    this.options = [];
  }

  command(signature) {
    const child = new FakeCommand(signature);
    this.children.push(child);
    return child;
  }

  description(value) {
    this.descriptionText = value;
    return this;
  }

  option(...args) {
    this.options.push(args);
    return this;
  }

  action(handler) {
    this.actionHandler = handler;
    return this;
  }

  find(signature) {
    const child = this.children.find((candidate) => candidate.signature === signature);
    assert.ok(child, `${signature} command was not registered`);
    return child;
  }
}

async function captureJsonOutput(fn) {
  const originalLog = console.log;
  const lines = [];
  console.log = (line) => {
    lines.push(String(line));
  };
  try {
    await fn();
  } finally {
    console.log = originalLog;
  }
  assert.equal(lines.length, 1);
  return JSON.parse(lines[0]);
}

test("agent tool dispatches public actions to the runtime and redacts output", async () => {
  const { api, runtime, createCalls } = registerFullPlugin();
  assert.equal(createCalls.length, 0);

  const tool = api.tools[0];
  assert.equal(tool.name, "sip_voice");
  assert.deepEqual(
    tool.parameters.properties.action.anyOf.map((schema) => schema.const),
    ["status", "tail", "call", "speak", "hangup"],
  );

  const status = parseToolText(await tool.execute("tool_1", { action: "status" }));
  assert.equal(createCalls.length, 1);
  assert.equal(runtime.calls.at(-1).method, "getStatus");
  assert.equal(status.sipPassword, "[redacted]");
  assert.doesNotMatch(JSON.stringify(status), /provider-secret|token-secret|BearerSecret/);

  parseToolText(await tool.execute("tool_2", { action: "tail", limit: 3 }));
  assert.deepEqual(runtime.calls.at(-1), { method: "tail", limit: 3 });

  parseToolText(
    await tool.execute("tool_3", {
      action: "call",
      remote: { handle: "+15551234567", displayName: "Caller" },
      message: "hello there",
    }),
  );
  assert.deepEqual(runtime.calls.at(-1), {
    method: "call",
    target: { handle: "+15551234567", displayName: "Caller" },
    message: "hello there",
  });

  parseToolText(
    await tool.execute("tool_4", {
      action: "speak",
      callId: "call_test001",
      message: "follow up",
    }),
  );
  assert.deepEqual(runtime.calls.at(-1), {
    method: "speak",
    callId: "call_test001",
    message: "follow up",
  });

  parseToolText(
    await tool.execute("tool_5", {
      action: "hangup",
      callId: "call_test001",
    }),
  );
  assert.deepEqual(runtime.calls.at(-1), {
    method: "hangup",
    callId: "call_test001",
  });

  const serialized = JSON.stringify([
    status,
    parseToolText(await tool.execute("tool_6", { action: "tail" })),
    parseToolText(
      await tool.execute("tool_7", {
        action: "speak",
        callId: "call_test001",
        message: "again",
      }),
    ),
  ]);
  assert.doesNotMatch(serialized, /tail-secret|speak-secret/);
});

test("agent tool rejects invalid inputs without calling runtime methods", async () => {
  const { api, runtime } = registerFullPlugin();
  const tool = api.tools[0];

  const missingTarget = parseToolText(
    await tool.execute("tool_1", { action: "call", message: "hello" }),
  );
  assert.equal(missingTarget.invalidInput, true);
  assert.match(missingTarget.error, /target or remote is required/);
  assert.deepEqual(runtime.calls, []);

  const badTail = parseToolText(
    await tool.execute("tool_2", { action: "tail", limit: "3" }),
  );
  assert.equal(badTail.invalidInput, true);
  assert.match(badTail.error, /limit must be an integer/);
  assert.deepEqual(runtime.calls, []);

  const unsupported = parseToolText(
    await tool.execute("tool_3", { action: "transfer", callId: "call_test001" }),
  );
  assert.equal(unsupported.invalidInput, true);
  assert.match(unsupported.error, /unsupported action: transfer/);
  assert.deepEqual(runtime.calls, []);
});

test("gateway registers the P5 target set and dispatches to runtime methods", async () => {
  const { api, runtime } = registerFullPlugin();
  assert.deepEqual([...api.gatewayMethods.keys()].sort(), [
    "sipvoice.call",
    "sipvoice.hangup",
    "sipvoice.speak",
    "sipvoice.status",
    "sipvoice.tail",
  ]);

  assert.equal((await invokeGateway(api, "sipvoice.status")).ok, true);
  assert.equal(runtime.calls.at(-1).method, "getStatus");

  assert.equal((await invokeGateway(api, "sipvoice.tail", { limit: 2 })).ok, true);
  assert.deepEqual(runtime.calls.at(-1), { method: "tail", limit: 2 });

  assert.equal(
    (await invokeGateway(api, "sipvoice.call", {
      target: "+15557654321",
      message: "start",
    })).ok,
    true,
  );
  assert.deepEqual(runtime.calls.at(-1), {
    method: "call",
    target: "+15557654321",
    message: "start",
  });

  assert.equal(
    (await invokeGateway(api, "sipvoice.speak", {
      callId: "call_test001",
      message: "say this",
    })).ok,
    true,
  );
  assert.deepEqual(runtime.calls.at(-1), {
    method: "speak",
    callId: "call_test001",
    message: "say this",
  });

  assert.equal(
    (await invokeGateway(api, "sipvoice.hangup", { callId: "call_test001" })).ok,
    true,
  );
  assert.deepEqual(runtime.calls.at(-1), {
    method: "hangup",
    callId: "call_test001",
  });

  const invalid = await invokeGateway(api, "sipvoice.speak", {
    callId: "call_test001",
  });
  assert.equal(invalid.ok, false);
  assert.equal(invalid.payload.invalidInput, true);
  assert.match(invalid.payload.error, /message is required/);
});

test("cli registers subcommands and dispatches actions", async () => {
  const { api, runtime } = registerFullPlugin();
  const program = new FakeCommand("<root>");
  api.cliRegistrations[0].callback({ program });

  const command = program.find("sipvoice");
  assert.deepEqual(command.children.map((child) => child.signature), [
    "status",
    "tail [limit]",
    "call <target> [message...]",
    "speak <callId> <message...>",
    "hangup <callId>",
  ]);

  await captureJsonOutput(() => command.actionHandler());
  assert.equal(runtime.calls.at(-1).method, "getStatus");

  await captureJsonOutput(() => command.find("status").actionHandler());
  assert.equal(runtime.calls.at(-1).method, "getStatus");

  await captureJsonOutput(() =>
    command.find("tail [limit]").actionHandler(undefined, { limit: "4" }),
  );
  assert.deepEqual(runtime.calls.at(-1), { method: "tail", limit: 4 });

  await captureJsonOutput(() =>
    command.find("call <target> [message...]").actionHandler("+15550001111", [
      "hello",
      "there",
    ]),
  );
  assert.deepEqual(runtime.calls.at(-1), {
    method: "call",
    target: "+15550001111",
    message: "hello there",
  });

  await captureJsonOutput(() =>
    command.find("speak <callId> <message...>").actionHandler("call_test001", [
      "follow",
      "up",
    ]),
  );
  assert.deepEqual(runtime.calls.at(-1), {
    method: "speak",
    callId: "call_test001",
    message: "follow up",
  });

  await captureJsonOutput(() =>
    command.find("hangup <callId>").actionHandler("call_test001"),
  );
  assert.deepEqual(runtime.calls.at(-1), {
    method: "hangup",
    callId: "call_test001",
  });

  await assert.rejects(
    () => command.find("tail [limit]").actionHandler("not-a-number", {}),
    /limit must be an integer/,
  );
});

test("discovery modes do not create a runtime or connect to the bridge", async () => {
  for (const mode of ["cli-metadata", "tool-discovery", "discovery"]) {
    const { createCalls } = installRuntimeFactory();
    const api = makeApi({ mode });
    pluginEntry.register(api);
    assert.equal(createCalls.length, 0, `${mode} should not create a runtime`);

    if (mode === "cli-metadata") {
      assert.equal(api.tools.length, 0);
      assert.equal(api.gatewayMethods.size, 0);
      continue;
    }

    const status = parseToolText(
      await api.tools[0].execute("tool_1", { action: "status" }),
    );
    assert.equal(status.enabled, true);
    assert.equal(status.runtimeAvailable, false);
    assert.equal(createCalls.length, 0, `${mode} status should stay lightweight`);

    const call = parseToolText(
      await api.tools[0].execute("tool_2", {
        action: "call",
        target: "+15551234567",
      }),
    );
    assert.equal(call.invalidInput, true);
    assert.match(call.error, /runtime is unavailable/);
    assert.equal(createCalls.length, 0, `${mode} call should stay lightweight`);
  }
});
