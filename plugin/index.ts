import { Type } from "typebox";
import {
  definePluginEntry,
  type OpenClawPluginApi,
} from "openclaw/plugin-sdk/plugin-entry";
import {
  parseSipVoiceConfig,
  sipVoicePluginConfigSchema,
  SIP_VOICE_CLI_COMMAND,
  SIP_VOICE_PLUGIN_ID,
  SIP_VOICE_TOOL_NAME,
  summarizeSipVoiceConfig,
  type SipVoiceConfig,
} from "./src/config.js";
import { createSipVoiceRuntime, type SipVoiceRuntime } from "./src/runtime.js";

const SIP_VOICE_ACTIONS = ["status", "tail", "call", "speak", "hangup"] as const;
type SipVoiceAction = (typeof SIP_VOICE_ACTIONS)[number];

const SIP_VOICE_CLI_DESCRIPTOR = {
  name: SIP_VOICE_CLI_COMMAND,
  description: "Inspect and control the SIP voice plugin.",
  hasSubcommands: true,
  subcommands: [
    { name: "status", description: "Print SIP voice plugin status." },
    { name: "tail", description: "Print recent SIP voice runtime events." },
    { name: "call", description: "Place an outbound SIP voice call." },
    { name: "speak", description: "Send speech text to an active call." },
    { name: "hangup", description: "Hang up an active SIP voice call." },
  ],
};

const RemotePartySchema = Type.Object(
  {
    handle: Type.String({
      minLength: 1,
      description: "Remote phone number, extension, or SIP bridge handle.",
    }),
    displayName: Type.Optional(
      Type.String({ minLength: 1, description: "Optional remote display name." }),
    ),
  },
  { additionalProperties: false },
);

const SipVoiceToolSchema = Type.Object({
  action: Type.Optional(
    Type.Union(
      [
        Type.Literal("status"),
        Type.Literal("tail"),
        Type.Literal("call"),
        Type.Literal("speak"),
        Type.Literal("hangup"),
      ],
      {
        default: "status",
        description: "SIP voice action to run.",
      },
    ),
  ),
  limit: Type.Optional(
    Type.Integer({
      minimum: 0,
      maximum: 50,
      description: "Maximum recent events/errors to return for tail.",
    }),
  ),
  target: Type.Optional(
    Type.String({
      minLength: 1,
      description: "Outbound call target handle for call action.",
    }),
  ),
  remote: Type.Optional(RemotePartySchema),
  message: Type.Optional(
    Type.String({
      minLength: 1,
      description: "Speech text for call intro or speak action.",
    }),
  ),
  callId: Type.Optional(
    Type.String({
      minLength: 1,
      description: "Active call id for speak and hangup actions.",
    }),
  ),
}, { additionalProperties: false });

const SENSITIVE_KEY_RE =
  /(sip|sdp|rtp|unifi|gemini|openai|credential|secret|token|password|authorization|apikey|api_key|api-key)/i;
const SENSITIVE_TEXT_RE =
  /\b((?:sip|sdp|rtp|unifi|gemini|openai|credential|secret|token|password|authorization|apikey|api_key|api-key)[A-Za-z0-9_.-]*)(\s*[:=]\s*)([^,\s;]+)/gi;

class SipVoicePublicInputError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "SipVoicePublicInputError";
  }
}

type RemotePartyInput = Parameters<SipVoiceRuntime["call"]>[0];

type SipVoiceActionRequest = {
  action?: unknown;
  limit?: unknown;
  target?: unknown;
  remote?: unknown;
  message?: unknown;
  callId?: unknown;
};

function jsonResult(payload: unknown) {
  const details = sanitizePublicPayload(payload);
  const text = JSON.stringify(details, null, 2);
  return {
    content: [
      {
        type: "text" as const,
        text,
      },
    ],
    details: JSON.parse(text),
  };
}

function redactSensitiveText(value: string): string {
  return value.replace(SENSITIVE_TEXT_RE, "$1$2[redacted]");
}

function sanitizePublicPayload<T>(value: T): T {
  if (value === null || value === undefined) {
    return value;
  }
  if (typeof value === "string") {
    return redactSensitiveText(value) as T;
  }
  if (Array.isArray(value)) {
    return value.map((item) => sanitizePublicPayload(item)) as T;
  }
  if (typeof value !== "object") {
    return value;
  }

  const sanitized: Record<string, unknown> = {};
  for (const [key, child] of Object.entries(value as Record<string, unknown>)) {
    sanitized[key] = SENSITIVE_KEY_RE.test(key)
      ? "[redacted]"
      : sanitizePublicPayload(child);
  }
  return sanitized as T;
}

function asRecord(value: unknown): Record<string, unknown> {
  if (value && typeof value === "object" && !Array.isArray(value)) {
    return value as Record<string, unknown>;
  }
  throw new SipVoicePublicInputError("parameters must be a JSON object");
}

function optionalNonEmptyString(
  value: unknown,
  field: string,
): string | undefined {
  if (value === undefined) {
    return undefined;
  }
  if (typeof value !== "string") {
    throw new SipVoicePublicInputError(`${field} must be a string`);
  }
  const trimmed = value.trim();
  if (!trimmed) {
    throw new SipVoicePublicInputError(`${field} must not be empty`);
  }
  return trimmed;
}

function requiredNonEmptyString(value: unknown, field: string): string {
  const result = optionalNonEmptyString(value, field);
  if (result === undefined) {
    throw new SipVoicePublicInputError(`${field} is required`);
  }
  return result;
}

function readAction(params: Record<string, unknown>): SipVoiceAction {
  if (params.action === undefined) {
    return "status";
  }
  if (typeof params.action !== "string") {
    throw new SipVoicePublicInputError("action must be a string");
  }
  if (SIP_VOICE_ACTIONS.includes(params.action as SipVoiceAction)) {
    return params.action as SipVoiceAction;
  }
  throw new SipVoicePublicInputError(`unsupported action: ${params.action}`);
}

function readTailLimit(value: unknown): number | undefined {
  if (value === undefined) {
    return undefined;
  }
  if (
    typeof value !== "number" ||
    !Number.isInteger(value) ||
    value < 0 ||
    value > 50
  ) {
    throw new SipVoicePublicInputError("limit must be an integer from 0 to 50");
  }
  return value;
}

function readRemoteParty(params: Record<string, unknown>): RemotePartyInput {
  const target = optionalNonEmptyString(params.target, "target");
  const rawRemote = params.remote;

  if (target !== undefined && rawRemote !== undefined) {
    throw new SipVoicePublicInputError("provide either target or remote, not both");
  }
  if (target !== undefined) {
    return target;
  }
  if (rawRemote === undefined) {
    throw new SipVoicePublicInputError("target or remote is required");
  }

  const remote = asRecord(rawRemote);
  const handle = requiredNonEmptyString(remote.handle, "remote.handle");
  const displayName = optionalNonEmptyString(remote.displayName, "remote.displayName");
  return {
    handle,
    ...(displayName === undefined ? {} : { displayName }),
  };
}

function lightweightStatus(config: SipVoiceConfig, runtimeAvailable: boolean) {
  return {
    enabled: config.enabled,
    config: summarizeSipVoiceConfig(config),
    runtimeAvailable,
  };
}

async function requireRuntime(params: {
  config: SipVoiceConfig;
  ensureRuntime?: () => Promise<SipVoiceRuntime>;
}): Promise<SipVoiceRuntime> {
  if (!params.config.enabled) {
    throw new SipVoicePublicInputError("sip-voice is disabled in plugin config");
  }
  if (!params.ensureRuntime) {
    throw new SipVoicePublicInputError(
      "sip-voice runtime is unavailable in this registration mode",
    );
  }
  return params.ensureRuntime();
}

async function runSipVoiceAction(params: {
  config: SipVoiceConfig;
  ensureRuntime?: () => Promise<SipVoiceRuntime>;
  request?: unknown;
}) {
  const request =
    params.request === undefined ? {} : asRecord(params.request);
  const action = readAction(request);

  if (action === "status" && (!params.config.enabled || !params.ensureRuntime)) {
    return lightweightStatus(params.config, Boolean(params.ensureRuntime));
  }

  const runtime = await requireRuntime(params);
  if (action === "status") {
    return runtime.getStatus();
  }
  if (action === "tail") {
    return runtime.tail(readTailLimit(request.limit));
  }
  if (action === "call") {
    return runtime.call(
      readRemoteParty(request),
      optionalNonEmptyString(request.message, "message"),
    );
  }
  if (action === "speak") {
    return runtime.speak(
      requiredNonEmptyString(request.callId, "callId"),
      requiredNonEmptyString(request.message, "message"),
    );
  }
  return runtime.hangup(requiredNonEmptyString(request.callId, "callId"));
}

function errorPayload(error: unknown) {
  const message = error instanceof Error ? error.message : String(error);
  return {
    error: redactSensitiveText(message),
    invalidInput: error instanceof SipVoicePublicInputError || undefined,
  };
}

function parseCliLimit(value: string | undefined): number | undefined {
  if (value === undefined) {
    return undefined;
  }
  if (!/^\d+$/.test(value)) {
    throw new SipVoicePublicInputError("limit must be an integer from 0 to 50");
  }
  return readTailLimit(Number(value));
}

function printJson(payload: unknown) {
  console.log(JSON.stringify(sanitizePublicPayload(payload), null, 2));
}

function registerSipVoiceCliMetadata(api: OpenClawPluginApi) {
  api.registerCli(() => {}, {
    commands: [SIP_VOICE_CLI_COMMAND],
    descriptors: [SIP_VOICE_CLI_DESCRIPTOR],
  });
}

function registerSipVoiceCli(
  api: OpenClawPluginApi,
  params: {
    config: SipVoiceConfig;
    ensureRuntime: () => Promise<SipVoiceRuntime>;
  },
) {
  api.registerCli(
    ({ program }) => {
      const command = program
        .command(SIP_VOICE_CLI_COMMAND)
        .description(SIP_VOICE_CLI_DESCRIPTOR.description)
        .action(async () => {
          printJson(await runSipVoiceAction(params));
        });

      command
        .command("status")
        .description("Print SIP voice plugin status.")
        .action(async () => {
          printJson(await runSipVoiceAction(params));
        });

      command
        .command("tail [limit]")
        .description("Print recent SIP voice runtime events.")
        .option("-n, --limit <limit>", "Maximum recent events/errors to return.")
        .action(async (limitArg: string | undefined, options: { limit?: string }) => {
          printJson(
            await runSipVoiceAction({
              ...params,
              request: {
                action: "tail",
                limit: parseCliLimit(options.limit ?? limitArg),
              },
            }),
          );
        });

      command
        .command("call <target> [message...]")
        .description("Place an outbound SIP voice call.")
        .action(async (target: string, messageParts: string[] | undefined) => {
          printJson(
            await runSipVoiceAction({
              ...params,
              request: {
                action: "call",
                target,
                message: messageParts?.join(" ") || undefined,
              },
            }),
          );
        });

      command
        .command("speak <callId> <message...>")
        .description("Send speech text to an active call.")
        .action(async (callId: string, messageParts: string[]) => {
          printJson(
            await runSipVoiceAction({
              ...params,
              request: {
                action: "speak",
                callId,
                message: messageParts.join(" "),
              },
            }),
          );
        });

      command
        .command("hangup <callId>")
        .description("Hang up an active SIP voice call.")
        .action(async (callId: string) => {
          printJson(
            await runSipVoiceAction({
              ...params,
              request: { action: "hangup", callId },
            }),
          );
        });
    },
    {
      commands: [SIP_VOICE_CLI_COMMAND],
      descriptors: [SIP_VOICE_CLI_DESCRIPTOR],
    },
  );
}

function registerSipVoiceTool(params: {
  api: OpenClawPluginApi;
  config: SipVoiceConfig;
  ensureRuntime?: () => Promise<SipVoiceRuntime>;
}) {
  params.api.registerTool({
    name: SIP_VOICE_TOOL_NAME,
    label: "SIP Voice",
    description:
      "Inspect and control SIP voice calls. This plugin does not store SIP credentials or RTP/SDP details.",
    parameters: SipVoiceToolSchema,
    async execute(_toolCallId, rawParams: SipVoiceActionRequest | undefined) {
      try {
        return jsonResult(
          await runSipVoiceAction({
            config: params.config,
            ensureRuntime: params.ensureRuntime,
            request: rawParams,
          }),
        );
      } catch (error) {
        return jsonResult(errorPayload(error));
      }
    },
  });
}

function registerSipVoiceGateway(params: {
  api: OpenClawPluginApi;
  config: SipVoiceConfig;
  ensureRuntime: () => Promise<SipVoiceRuntime>;
}) {
  const registerMethod = (method: string, action: SipVoiceAction) => {
    params.api.registerGatewayMethod(
      method,
      async ({ params: requestParams, respond }) => {
        const request =
          requestParams && typeof requestParams === "object"
            ? { ...(requestParams as Record<string, unknown>), action }
            : { action };
        try {
          respond(
            true,
            sanitizePublicPayload(
              await runSipVoiceAction({
                config: params.config,
                ensureRuntime: params.ensureRuntime,
                request,
              }),
            ),
          );
        } catch (error) {
          respond(false, errorPayload(error));
        }
      },
    );
  };

  registerMethod("sipvoice.status", "status");
  registerMethod("sipvoice.tail", "tail");
  registerMethod("sipvoice.call", "call");
  registerMethod("sipvoice.speak", "speak");
  registerMethod("sipvoice.hangup", "hangup");
}

export default definePluginEntry({
  id: SIP_VOICE_PLUGIN_ID,
  name: "SIP Voice",
  description: "OpenClaw SIP voice plugin foundation",
  configSchema: sipVoicePluginConfigSchema,
  register(api: OpenClawPluginApi) {
    const config = parseSipVoiceConfig(api.pluginConfig);

    if (api.registrationMode === "cli-metadata") {
      registerSipVoiceCliMetadata(api);
      return;
    }

    if (api.registrationMode === "tool-discovery") {
      registerSipVoiceTool({ api, config });
      return;
    }

    if (api.registrationMode === "discovery") {
      registerSipVoiceCliMetadata(api);
      registerSipVoiceTool({ api, config });
      return;
    }

    if (api.registrationMode !== "full") {
      return;
    }

    let runtimePromise: Promise<SipVoiceRuntime> | null = null;
    let runtime: SipVoiceRuntime | null = null;

    const ensureRuntime = async () => {
      if (!config.enabled) {
        throw new Error("sip-voice is disabled in plugin config");
      }
      if (runtime) {
        return runtime;
      }
      runtimePromise ??= createSipVoiceRuntime({
        config,
        fullConfig: api.config,
        runtime: api.runtime,
        logger: api.logger,
      });
      runtime = await runtimePromise;
      return runtime;
    };

    registerSipVoiceCli(api, { config, ensureRuntime });
    registerSipVoiceTool({ api, config, ensureRuntime });
    registerSipVoiceGateway({ api, config, ensureRuntime });

    api.registerService({
      id: SIP_VOICE_PLUGIN_ID,
      start: async () => {
        if (!config.enabled) {
          return;
        }
        await ensureRuntime();
      },
      stop: async () => {
        await runtime?.stop();
        runtime = null;
        runtimePromise = null;
      },
    });
  },
});
