import {
  buildJsonPluginConfigSchema,
  type OpenClawPluginConfigSchema,
} from "openclaw/plugin-sdk/plugin-entry";
import type { RealtimeVoiceProviderConfig } from "openclaw/plugin-sdk/realtime-voice";

export const SIP_VOICE_PLUGIN_ID = "sip-voice";
export const SIP_VOICE_TOOL_NAME = "sip_voice";
export const SIP_VOICE_CLI_COMMAND = "sipvoice";

export const SIP_VOICE_AUDIO_FORMAT_G711_ULAW_8KHZ = "g711-ulaw-8khz";

export const SIP_VOICE_DEFAULTS = {
  enabled: true,
  bridge: {
    url: "ws://127.0.0.1:9077",
  },
  maxConcurrentCalls: 1,
  audio: {
    format: SIP_VOICE_AUDIO_FORMAT_G711_ULAW_8KHZ,
  },
  realtime: {
    provider: "google",
    model: "gemini-3.1-flash-live-preview",
    toolPolicy: "safe-read-only",
    providers: {},
  },
} as const;

export type SipVoiceAudioFormat = typeof SIP_VOICE_AUDIO_FORMAT_G711_ULAW_8KHZ;

export type SipVoiceRealtimeConfig = {
  provider: string;
  model: string;
  toolPolicy: string;
  providers: Record<string, RealtimeVoiceProviderConfig | undefined>;
  instructions?: string;
  introMessage?: string;
};

export type SipVoiceConfig = {
  enabled: boolean;
  bridge: {
    url: string;
  };
  maxConcurrentCalls: number;
  audio: {
    format: SipVoiceAudioFormat;
  };
  realtime: SipVoiceRealtimeConfig;
};

export type SipVoiceConfigSummary = Omit<SipVoiceConfig, "realtime"> & {
  realtime: Omit<SipVoiceRealtimeConfig, "providers"> & {
    providerConfigCount: number;
  };
};

const sipVoiceConfigJsonSchema = {
  type: "object",
  additionalProperties: false,
  properties: {
    enabled: {
      type: "boolean",
      default: SIP_VOICE_DEFAULTS.enabled,
    },
    bridge: {
      type: "object",
      additionalProperties: false,
      default: SIP_VOICE_DEFAULTS.bridge,
      properties: {
        url: {
          type: "string",
          default: SIP_VOICE_DEFAULTS.bridge.url,
        },
      },
    },
    maxConcurrentCalls: {
      type: "integer",
      minimum: 1,
      default: SIP_VOICE_DEFAULTS.maxConcurrentCalls,
    },
    audio: {
      type: "object",
      additionalProperties: false,
      default: SIP_VOICE_DEFAULTS.audio,
      properties: {
        format: {
          type: "string",
          enum: [SIP_VOICE_AUDIO_FORMAT_G711_ULAW_8KHZ],
          default: SIP_VOICE_DEFAULTS.audio.format,
        },
      },
    },
    realtime: {
      type: "object",
      additionalProperties: false,
      default: SIP_VOICE_DEFAULTS.realtime,
      properties: {
        provider: {
          type: "string",
          default: SIP_VOICE_DEFAULTS.realtime.provider,
        },
        model: {
          type: "string",
          default: SIP_VOICE_DEFAULTS.realtime.model,
        },
        toolPolicy: {
          type: "string",
          default: SIP_VOICE_DEFAULTS.realtime.toolPolicy,
        },
        providers: {
          type: "object",
          additionalProperties: {
            type: "object",
            additionalProperties: true,
          },
          default: SIP_VOICE_DEFAULTS.realtime.providers,
        },
        instructions: {
          type: "string",
        },
        introMessage: {
          type: "string",
        },
      },
    },
  },
} as const;

export const sipVoicePluginConfigSchema: OpenClawPluginConfigSchema = buildJsonPluginConfigSchema(
  sipVoiceConfigJsonSchema,
  {
    cacheKey: "sip-voice.config",
    uiHints: {
      enabled: { label: "Enabled" },
      "bridge.url": {
        label: "Bridge URL",
        help: "Local SIP voice bridge websocket URL. The plugin does not store SIP credentials.",
      },
      maxConcurrentCalls: { label: "Max Concurrent Calls" },
      "audio.format": { label: "Audio Format", advanced: true },
      "realtime.provider": { label: "Realtime Provider", advanced: true },
      "realtime.model": { label: "Realtime Model", advanced: true },
      "realtime.toolPolicy": {
        label: "Realtime Tool Policy",
        help: "Controls realtime assistant tool access.",
        advanced: true,
      },
      "realtime.providers": { label: "Realtime Provider Config", advanced: true },
      "realtime.instructions": { label: "Realtime Instructions", advanced: true },
      "realtime.introMessage": { label: "Realtime Intro Message", advanced: true },
    },
  },
);

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {};
}

function readString(value: unknown, fallback: string): string {
  const trimmed = typeof value === "string" ? value.trim() : "";
  return trimmed || fallback;
}

function readOptionalString(value: unknown): string | undefined {
  return typeof value === "string" ? value : undefined;
}

function assertWebsocketUrl(value: string): string {
  let parsed: URL;
  try {
    parsed = new URL(value);
  } catch {
    throw new Error("bridge.url must be a valid ws:// or wss:// URL");
  }
  if (parsed.protocol !== "ws:" && parsed.protocol !== "wss:") {
    throw new Error("bridge.url must use ws:// or wss://");
  }
  return value;
}

function formatConfigError(result: {
  success: boolean;
  error?: { issues?: Array<{ path: Array<string | number>; message: string }> };
}): string {
  const issues = result.error?.issues ?? [];
  if (issues.length === 0) {
    return "invalid sip-voice config";
  }
  return issues
    .map((issue) => {
      const path = issue.path.length > 0 ? issue.path.join(".") : "<root>";
      return `${path}: ${issue.message}`;
    })
    .join("; ");
}

export function parseSipVoiceConfig(value: unknown): SipVoiceConfig {
  const result = sipVoicePluginConfigSchema.safeParse?.(value ?? {});
  if (!result?.success) {
    throw new Error(formatConfigError(result ?? { success: false }));
  }

  const raw = asRecord(result.data);
  const bridge = asRecord(raw.bridge);
  const audio = asRecord(raw.audio);
  const realtime = asRecord(raw.realtime);
  const providers = asRecord(realtime.providers);
  const format = readString(audio.format, SIP_VOICE_DEFAULTS.audio.format);

  if (format !== SIP_VOICE_AUDIO_FORMAT_G711_ULAW_8KHZ) {
    throw new Error(`audio.format must be ${SIP_VOICE_AUDIO_FORMAT_G711_ULAW_8KHZ}`);
  }

  return {
    enabled: raw.enabled === undefined ? SIP_VOICE_DEFAULTS.enabled : raw.enabled === true,
    bridge: {
      url: assertWebsocketUrl(readString(bridge.url, SIP_VOICE_DEFAULTS.bridge.url)),
    },
    maxConcurrentCalls:
      typeof raw.maxConcurrentCalls === "number"
        ? raw.maxConcurrentCalls
        : SIP_VOICE_DEFAULTS.maxConcurrentCalls,
    audio: {
      format,
    },
    realtime: {
      provider: readString(realtime.provider, SIP_VOICE_DEFAULTS.realtime.provider),
      model: readString(realtime.model, SIP_VOICE_DEFAULTS.realtime.model),
      toolPolicy: readString(realtime.toolPolicy, SIP_VOICE_DEFAULTS.realtime.toolPolicy),
      providers: providers as Record<string, RealtimeVoiceProviderConfig | undefined>,
      instructions: readOptionalString(realtime.instructions),
      introMessage: readOptionalString(realtime.introMessage),
    },
  };
}

export function summarizeSipVoiceConfig(config: SipVoiceConfig): SipVoiceConfigSummary {
  return {
    enabled: config.enabled,
    bridge: {
      url: config.bridge.url,
    },
    maxConcurrentCalls: config.maxConcurrentCalls,
    audio: {
      format: config.audio.format,
    },
    realtime: {
      provider: config.realtime.provider,
      model: config.realtime.model,
      toolPolicy: config.realtime.toolPolicy,
      instructions: config.realtime.instructions,
      introMessage: config.realtime.introMessage,
      providerConfigCount: Object.keys(config.realtime.providers).length,
    },
  };
}
