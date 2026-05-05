import type {
  AudioClearCommand,
  AudioFormat,
  AudioFrame,
  AudioInEvent,
  AudioOutCommand,
  BridgeEvent,
  CallAnswerCommand,
  CallDialCommand,
  CallEndEvent,
  CallHangupCommand,
  CallStartEvent,
  CommandEnvelope,
  ErrorEvent,
  HelloEvent,
  PluginCommand,
  ProtocolMessage,
  ProtocolVersion,
  RemoteParty,
  StatusEvent,
  StatusGetCommand,
} from "../../protocol/types/messages.js";

export type {
  AudioClearCommand,
  AudioFormat,
  AudioFrame,
  AudioInEvent,
  AudioOutCommand,
  BridgeEvent,
  CallAnswerCommand,
  CallDialCommand,
  CallEndEvent,
  CallHangupCommand,
  CallStartEvent,
  CommandEnvelope,
  ErrorEvent,
  HelloEvent,
  PluginCommand,
  ProtocolMessage,
  ProtocolVersion,
  RemoteParty,
  StatusEvent,
  StatusGetCommand,
} from "../../protocol/types/messages.js";

export const PROTOCOL_VERSION = "1.0" as const satisfies ProtocolVersion;
export const SUPPORTED_PROTOCOL_VERSIONS = [PROTOCOL_VERSION] as const;
export const DEFAULT_BRIDGE_URL = "ws://127.0.0.1:9077" as const;
export const UNSUPPORTED_VERSION_CLOSE_CODE = 1002 as const;

export const AUDIO_FRAME_BYTES = 160 as const;
export const AUDIO_PAYLOAD_BASE64_CHARS = 216 as const;
export const SIP_VOICE_AUDIO_FORMAT = {
  format: "g711_ulaw",
  sampleRateHz: 8000,
  channels: 1,
  frameDurationMs: 20,
  payloadEncoding: "base64",
} as const satisfies AudioFormat;

export const BRIDGE_EVENT_TYPES = [
  "hello",
  "status",
  "call.start",
  "audio.in",
  "call.end",
  "error",
] as const;

export const PLUGIN_COMMAND_TYPES = [
  "status.get",
  "call.answer",
  "call.dial",
  "audio.out",
  "audio.clear",
  "call.hangup",
] as const;

export type BridgeEventType = (typeof BRIDGE_EVENT_TYPES)[number];
export type PluginCommandType = (typeof PLUGIN_COMMAND_TYPES)[number];
export type ProtocolMessageType = BridgeEventType | PluginCommandType;

export type ProtocolValidationCode =
  | "unsupported_protocol_version"
  | "validation_failed";

export type ProtocolValidationIssue = {
  path: string;
  message: string;
};

export type ProtocolValidationSuccess<T> = {
  ok: true;
  value: T;
  errors: [];
};

export type ProtocolValidationFailure = {
  ok: false;
  error: ProtocolValidationError;
  errors: ProtocolValidationIssue[];
};

export type ProtocolValidationResult<T> =
  | ProtocolValidationSuccess<T>
  | ProtocolValidationFailure;

export type BuildCommandOptions = {
  commandId?: string;
  sentAt?: string;
};

export type BuildCallAnswerCommandOptions = BuildCommandOptions & {
  callId: string;
};

export type BuildCallDialCommandOptions = BuildCommandOptions & {
  remote: RemoteParty;
  audio?: AudioFormat;
};

export type BuildAudioOutCommandOptions = BuildCommandOptions & {
  callId: string;
  sequence: number;
  audio?: AudioFrame;
  payload?: string | Uint8Array;
};

export type BuildAudioClearCommandOptions = BuildCommandOptions & {
  callId: string;
  reason?: AudioClearCommand["reason"];
};

export type BuildCallHangupCommandOptions = BuildCommandOptions & {
  callId: string;
  reason?: CallHangupCommand["reason"];
};

type Validator = (
  value: unknown,
  pathLabel: string,
  errors: ProtocolValidationIssue[],
) => void;

type ObjectSpec = {
  required: readonly string[];
  optional?: readonly string[];
  properties: Record<string, Validator>;
  after?: (
    value: Record<string, unknown>,
    pathLabel: string,
    errors: ProtocolValidationIssue[],
  ) => void;
};

const BASE64_RE =
  /^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/;
const CALL_ID_RE = /^call_[A-Za-z0-9._:-]{6,96}$/;
const COMMAND_ID_RE = /^cmd_[A-Za-z0-9._:-]{6,96}$/;
const BRIDGE_ID_RE = /^bridge_[A-Za-z0-9._:-]{3,96}$/;
const REASON_CODE_RE = /^[a-z][a-z0-9_]{1,63}$/;
const REMOTE_HANDLE_RE = /^[+A-Za-z0-9][A-Za-z0-9 ._()+-]{0,63}$/;
const FORBIDDEN_FIELD_NAME_RE =
  /(sip|sdp|rtp|unifi|gemini|openai|credential|secret|token|password|authorization|apikey|api_key|api-key)/i;

const AUDIO_FORMAT_SPEC: ObjectSpec = {
  required: [
    "format",
    "sampleRateHz",
    "channels",
    "frameDurationMs",
    "payloadEncoding",
  ],
  properties: {
    format: literal("g711_ulaw"),
    sampleRateHz: literal(8000),
    channels: literal(1),
    frameDurationMs: literal(20),
    payloadEncoding: literal("base64"),
  },
};

const AUDIO_FRAME_SPEC: ObjectSpec = {
  required: [...AUDIO_FORMAT_SPEC.required, "payload"],
  properties: {
    ...AUDIO_FORMAT_SPEC.properties,
    payload: base64AudioPayload,
  },
};

const MESSAGE_SPECS: Record<string, ObjectSpec> = {
  hello: {
    required: [
      "protocolVersion",
      "type",
      "sentAt",
      "bridgeId",
      "bridgeVersion",
      "transport",
      "supportedProtocolVersions",
      "audio",
      "capabilities",
    ],
    properties: {
      protocolVersion: literal(PROTOCOL_VERSION),
      type: literal("hello"),
      sentAt: timestamp,
      bridgeId: patternString(BRIDGE_ID_RE, "bridge id"),
      bridgeVersion: boundedString(1, 40),
      transport: exactObject({
        required: ["kind", "defaultUrl"],
        properties: {
          kind: literal("websocket"),
          defaultUrl: literal(DEFAULT_BRIDGE_URL),
        },
      }),
      supportedProtocolVersions,
      audio: exactObject(AUDIO_FORMAT_SPEC),
      capabilities: exactObject({
        required: [
          "inboundCalls",
          "outboundCalls",
          "bargeIn",
          "clearQueuedAudio",
        ],
        properties: {
          inboundCalls: booleanValue,
          outboundCalls: booleanValue,
          bargeIn: booleanValue,
          clearQueuedAudio: booleanValue,
        },
      }),
    },
  },
  status: {
    required: [
      "protocolVersion",
      "type",
      "sentAt",
      "bridgeState",
      "registration",
      "activeCalls",
    ],
    properties: {
      protocolVersion: literal(PROTOCOL_VERSION),
      type: literal("status"),
      sentAt: timestamp,
      bridgeState: enumValue(["starting", "ready", "degraded", "offline"]),
      registration: exactObject({
        required: ["state"],
        optional: ["label", "reasonCode", "message"],
        properties: {
          state: enumValue(["registered", "unregistered", "registering", "error"]),
          label: boundedString(1, 80),
          reasonCode: patternString(REASON_CODE_RE, "reason code"),
          message: boundedString(1, 160),
        },
      }),
      activeCalls: arrayOf(callSummary, 16),
    },
  },
  "call.start": {
    required: [
      "protocolVersion",
      "type",
      "sentAt",
      "callId",
      "direction",
      "state",
      "remote",
      "audio",
    ],
    optional: ["requestedByCommandId"],
    properties: {
      protocolVersion: literal(PROTOCOL_VERSION),
      type: literal("call.start"),
      sentAt: timestamp,
      callId: patternString(CALL_ID_RE, "call id"),
      direction: enumValue(["inbound", "outbound"]),
      state: enumValue(["ringing", "dialing", "active"]),
      remote: remoteParty,
      audio: exactObject(AUDIO_FORMAT_SPEC),
      requestedByCommandId: patternString(COMMAND_ID_RE, "command id"),
    },
  },
  "audio.in": {
    required: [
      "protocolVersion",
      "type",
      "sentAt",
      "callId",
      "sequence",
      "timestampMs",
      "audio",
    ],
    properties: {
      protocolVersion: literal(PROTOCOL_VERSION),
      type: literal("audio.in"),
      sentAt: timestamp,
      callId: patternString(CALL_ID_RE, "call id"),
      sequence: integerAtLeast(0),
      timestampMs: integerAtLeast(0),
      audio: exactObject(AUDIO_FRAME_SPEC),
    },
  },
  "call.end": {
    required: ["protocolVersion", "type", "sentAt", "callId", "outcome"],
    optional: ["durationMs", "reason"],
    properties: {
      protocolVersion: literal(PROTOCOL_VERSION),
      type: literal("call.end"),
      sentAt: timestamp,
      callId: patternString(CALL_ID_RE, "call id"),
      outcome: enumValue(["completed", "error", "missed", "rejected", "canceled"]),
      durationMs: integerAtLeast(0),
      reason: exactObject({
        required: ["code"],
        optional: ["message"],
        properties: {
          code: patternString(REASON_CODE_RE, "reason code"),
          message: boundedString(1, 160),
        },
      }),
    },
    after(value, pathLabel, errors) {
      if (value.outcome === "error" && !hasOwn(value, "reason")) {
        errors.push(errorAt(`${pathLabel}.reason`, "reason is required when outcome is error"));
      }
    },
  },
  error: {
    required: ["protocolVersion", "type", "sentAt", "fatal", "error"],
    properties: {
      protocolVersion: literal(PROTOCOL_VERSION),
      type: literal("error"),
      sentAt: timestamp,
      fatal: booleanValue,
      error: exactObject({
        required: ["code", "message", "retryable"],
        optional: ["commandId", "callId", "expectedProtocolVersions"],
        properties: {
          code: enumValue([
            "unsupported_protocol_version",
            "validation_failed",
            "bridge_unavailable",
            "call_not_found",
            "call_rejected",
            "audio_underrun",
            "internal_error",
          ]),
          message: boundedString(1, 160),
          retryable: booleanValue,
          commandId: patternString(COMMAND_ID_RE, "command id"),
          callId: patternString(CALL_ID_RE, "call id"),
          expectedProtocolVersions: supportedProtocolVersions,
        },
        after(value, pathLabel, errors) {
          if (value.code !== "unsupported_protocol_version") {
            return;
          }
          if (!hasOwn(value, "expectedProtocolVersions")) {
            errors.push(
              errorAt(
                `${pathLabel}.expectedProtocolVersions`,
                "expectedProtocolVersions is required",
              ),
            );
          }
        },
      }),
    },
    after(value, pathLabel, errors) {
      const details = asRecord(value.error);
      if (details.code !== "unsupported_protocol_version") {
        return;
      }
      if (value.fatal !== true) {
        errors.push(errorAt(`${pathLabel}.fatal`, "unsupported protocol version errors must be fatal"));
      }
      if (details.retryable !== false) {
        errors.push(
          errorAt(`${pathLabel}.error.retryable`, "unsupported protocol version errors are not retryable"),
        );
      }
    },
  },
  "status.get": commandSpec("status.get", {}, []),
  "call.answer": commandSpec(
    "call.answer",
    { callId: patternString(CALL_ID_RE, "call id") },
    ["callId"],
  ),
  "call.dial": commandSpec(
    "call.dial",
    {
      remote: remoteParty,
      audio: exactObject(AUDIO_FORMAT_SPEC),
    },
    ["remote", "audio"],
  ),
  "audio.out": commandSpec(
    "audio.out",
    {
      callId: patternString(CALL_ID_RE, "call id"),
      sequence: integerAtLeast(0),
      audio: exactObject(AUDIO_FRAME_SPEC),
    },
    ["callId", "sequence", "audio"],
  ),
  "audio.clear": commandSpec(
    "audio.clear",
    {
      callId: patternString(CALL_ID_RE, "call id"),
      scope: literal("queued"),
      reason: enumValue(["barge_in", "user_request", "call_ending"]),
    },
    ["callId", "scope"],
    ["reason"],
  ),
  "call.hangup": commandSpec(
    "call.hangup",
    {
      callId: patternString(CALL_ID_RE, "call id"),
      reason: enumValue(["user_request", "completed", "failed"]),
    },
    ["callId"],
    ["reason"],
  ),
};

const BRIDGE_EVENT_TYPE_SET = new Set<string>(BRIDGE_EVENT_TYPES);
const PLUGIN_COMMAND_TYPE_SET = new Set<string>(PLUGIN_COMMAND_TYPES);

export class ProtocolValidationError extends Error {
  readonly code: ProtocolValidationCode;
  readonly errors: ProtocolValidationIssue[];
  readonly fatal: boolean;
  readonly closeCode?: number;
  readonly expectedProtocolVersions?: readonly ProtocolVersion[];

  constructor(
    message: string,
    params: {
      code: ProtocolValidationCode;
      errors: ProtocolValidationIssue[];
      fatal?: boolean;
      closeCode?: number;
      expectedProtocolVersions?: readonly ProtocolVersion[];
    },
  ) {
    super(message);
    this.name = "ProtocolValidationError";
    this.code = params.code;
    this.errors = params.errors;
    this.fatal = params.fatal ?? false;
    this.closeCode = params.closeCode;
    this.expectedProtocolVersions = params.expectedProtocolVersions;
  }
}

export function isBridgeEventType(type: unknown): type is BridgeEventType {
  return typeof type === "string" && BRIDGE_EVENT_TYPE_SET.has(type);
}

export function isPluginCommandType(type: unknown): type is PluginCommandType {
  return typeof type === "string" && PLUGIN_COMMAND_TYPE_SET.has(type);
}

export function createCommandId(): string {
  const timestamp = Date.now().toString(36);
  const random = Math.random().toString(36).slice(2, 12).padEnd(10, "0");
  return `cmd_${timestamp}_${random}`;
}

export function buildUnsupportedProtocolVersionErrorEvent(
  receivedVersion: unknown,
  sentAt = new Date().toISOString(),
): ErrorEvent {
  return {
    protocolVersion: PROTOCOL_VERSION,
    type: "error",
    sentAt,
    fatal: true,
    error: {
      code: "unsupported_protocol_version",
      message: `Unsupported protocol version: ${String(receivedVersion)}`,
      retryable: false,
      expectedProtocolVersions: [PROTOCOL_VERSION],
    },
  };
}

export function validateProtocolMessage(
  value: unknown,
): ProtocolValidationResult<ProtocolMessage> {
  if (!isPlainObject(value)) {
    return validationFailure([errorAt("$", "message must be a JSON object")]);
  }

  if (
    typeof value.protocolVersion === "string" &&
    value.protocolVersion !== PROTOCOL_VERSION
  ) {
    return unsupportedProtocolVersionFailure(value.protocolVersion);
  }

  const errors: ProtocolValidationIssue[] = [];

  if (value.protocolVersion !== PROTOCOL_VERSION) {
    errors.push(errorAt("$.protocolVersion", `protocolVersion must be ${PROTOCOL_VERSION}`));
  }

  for (const fieldPath of collectForbiddenFieldNames(value)) {
    errors.push(errorAt(fieldPath, "field name is not allowed across the plugin boundary"));
  }

  if (typeof value.type !== "string") {
    errors.push(errorAt("$.type", "type must be a string"));
    return validationFailure(errors);
  }

  const spec = MESSAGE_SPECS[value.type];
  if (!spec) {
    errors.push(errorAt("$.type", `unknown message type ${JSON.stringify(value.type)}`));
    return validationFailure(errors);
  }

  validateExactObject(value, "$", spec, errors);

  if (errors.length > 0) {
    return validationFailure(errors);
  }

  return { ok: true, value: value as ProtocolMessage, errors: [] };
}

export function validateBridgeEvent(
  value: unknown,
): ProtocolValidationResult<BridgeEvent> {
  const result = validateProtocolMessage(value);
  if (!result.ok) {
    return result;
  }
  if (!isBridgeEventType(result.value.type)) {
    return validationFailure([
      errorAt("$.type", `message type ${JSON.stringify(result.value.type)} is not a bridge event`),
    ]);
  }
  return { ok: true, value: result.value as BridgeEvent, errors: [] };
}

export function validatePluginCommand(
  value: unknown,
): ProtocolValidationResult<PluginCommand> {
  const result = validateProtocolMessage(value);
  if (!result.ok) {
    return result;
  }
  if (!isPluginCommandType(result.value.type)) {
    return validationFailure([
      errorAt("$.type", `message type ${JSON.stringify(result.value.type)} is not a plugin command`),
    ]);
  }
  return { ok: true, value: result.value as PluginCommand, errors: [] };
}

export function parseProtocolMessage(data: unknown): ProtocolMessage {
  return assertValidProtocolMessage(parseJsonMessage(data));
}

export function parseBridgeEvent(data: unknown): BridgeEvent {
  return assertValidBridgeEvent(parseJsonMessage(data));
}

export function assertValidProtocolMessage(value: unknown): ProtocolMessage {
  const result = validateProtocolMessage(value);
  if (!result.ok) {
    throw result.error;
  }
  return result.value;
}

export function assertValidBridgeEvent(value: unknown): BridgeEvent {
  const result = validateBridgeEvent(value);
  if (!result.ok) {
    throw result.error;
  }
  return result.value;
}

export function assertValidPluginCommand(value: unknown): PluginCommand {
  const result = validatePluginCommand(value);
  if (!result.ok) {
    throw result.error;
  }
  return result.value;
}

export function buildAudioFrame(payload: string | Uint8Array): AudioFrame {
  const frame = {
    ...SIP_VOICE_AUDIO_FORMAT,
    payload: typeof payload === "string" ? payload : Buffer.from(payload).toString("base64"),
  } satisfies AudioFrame;
  assertValidAudioFrame(frame);
  return frame;
}

export function buildStatusGetCommand(
  options: BuildCommandOptions = {},
): StatusGetCommand {
  return assertValidPluginCommand({
    ...commandEnvelope("status.get", options),
  }) as StatusGetCommand;
}

export function buildCallAnswerCommand(
  options: BuildCallAnswerCommandOptions,
): CallAnswerCommand {
  return assertValidPluginCommand({
    ...commandEnvelope("call.answer", options),
    callId: options.callId,
  }) as CallAnswerCommand;
}

export function buildCallDialCommand(
  options: BuildCallDialCommandOptions,
): CallDialCommand {
  return assertValidPluginCommand({
    ...commandEnvelope("call.dial", options),
    remote: options.remote,
    audio: options.audio ?? SIP_VOICE_AUDIO_FORMAT,
  }) as CallDialCommand;
}

export function buildAudioOutCommand(
  options: BuildAudioOutCommandOptions,
): AudioOutCommand {
  const audio =
    options.audio ?? (options.payload === undefined ? undefined : buildAudioFrame(options.payload));
  return assertValidPluginCommand({
    ...commandEnvelope("audio.out", options),
    callId: options.callId,
    sequence: options.sequence,
    audio,
  }) as AudioOutCommand;
}

export function buildAudioClearCommand(
  options: BuildAudioClearCommandOptions,
): AudioClearCommand {
  return assertValidPluginCommand({
    ...commandEnvelope("audio.clear", options),
    callId: options.callId,
    scope: "queued",
    ...(options.reason === undefined ? {} : { reason: options.reason }),
  }) as AudioClearCommand;
}

export function buildCallHangupCommand(
  options: BuildCallHangupCommandOptions,
): CallHangupCommand {
  return assertValidPluginCommand({
    ...commandEnvelope("call.hangup", options),
    callId: options.callId,
    ...(options.reason === undefined ? {} : { reason: options.reason }),
  }) as CallHangupCommand;
}

function commandEnvelope(
  type: PluginCommandType,
  options: BuildCommandOptions,
): CommandEnvelope {
  return {
    protocolVersion: PROTOCOL_VERSION,
    type,
    sentAt: options.sentAt ?? new Date().toISOString(),
    commandId: options.commandId ?? createCommandId(),
  };
}

function assertValidAudioFrame(value: AudioFrame): void {
  const errors: ProtocolValidationIssue[] = [];
  validateExactObject(value as unknown as Record<string, unknown>, "$.audio", AUDIO_FRAME_SPEC, errors);
  if (errors.length > 0) {
    throw createValidationError(errors);
  }
}

function parseJsonMessage(data: unknown): unknown {
  if (typeof data !== "string") {
    return data;
  }
  try {
    return JSON.parse(data);
  } catch {
    throw createValidationError([errorAt("$", "message must be valid JSON")]);
  }
}

function unsupportedProtocolVersionFailure(
  receivedVersion: string,
): ProtocolValidationFailure {
  const errors = [
    errorAt(
      "$.protocolVersion",
      `unsupported protocol version ${JSON.stringify(receivedVersion)}`,
    ),
  ];
  return {
    ok: false,
    error: new ProtocolValidationError("Unsupported protocol version", {
      code: "unsupported_protocol_version",
      errors,
      fatal: true,
      closeCode: UNSUPPORTED_VERSION_CLOSE_CODE,
      expectedProtocolVersions: SUPPORTED_PROTOCOL_VERSIONS,
    }),
    errors,
  };
}

function validationFailure(errors: ProtocolValidationIssue[]): ProtocolValidationFailure {
  return {
    ok: false,
    error: createValidationError(errors),
    errors,
  };
}

function createValidationError(errors: ProtocolValidationIssue[]): ProtocolValidationError {
  return new ProtocolValidationError("Invalid SIP voice bridge protocol message", {
    code: "validation_failed",
    errors,
  });
}

function commandSpec(
  type: PluginCommandType,
  properties: Record<string, Validator>,
  requiredCommandFields: readonly string[],
  optionalCommandFields: readonly string[] = [],
): ObjectSpec {
  return {
    required: ["protocolVersion", "type", "sentAt", "commandId", ...requiredCommandFields],
    optional: optionalCommandFields,
    properties: {
      protocolVersion: literal(PROTOCOL_VERSION),
      type: literal(type),
      sentAt: timestamp,
      commandId: patternString(COMMAND_ID_RE, "command id"),
      ...properties,
    },
  };
}

function validateExactObject(
  value: unknown,
  pathLabel: string,
  spec: ObjectSpec,
  errors: ProtocolValidationIssue[],
): void {
  if (!isPlainObject(value)) {
    errors.push(errorAt(pathLabel, "must be an object"));
    return;
  }

  const required = new Set(spec.required);
  const allowed = new Set([...spec.required, ...(spec.optional ?? [])]);

  for (const key of required) {
    if (!hasOwn(value, key)) {
      errors.push(errorAt(`${pathLabel}.${key}`, "is required"));
    }
  }

  for (const key of Object.keys(value)) {
    if (!allowed.has(key)) {
      errors.push(errorAt(`${pathLabel}.${key}`, "is not allowed"));
      continue;
    }

    const validator = spec.properties[key];
    validator?.(value[key], `${pathLabel}.${key}`, errors);
  }

  spec.after?.(value, pathLabel, errors);
}

function exactObject(spec: ObjectSpec): Validator {
  return (value, pathLabel, errors) => validateExactObject(value, pathLabel, spec, errors);
}

function callSummary(value: unknown, pathLabel: string, errors: ProtocolValidationIssue[]): void {
  validateExactObject(
    value,
    pathLabel,
    {
      required: ["callId", "direction", "state"],
      properties: {
        callId: patternString(CALL_ID_RE, "call id"),
        direction: enumValue(["inbound", "outbound"]),
        state: enumValue(["ringing", "dialing", "active", "ending"]),
      },
    },
    errors,
  );
}

function remoteParty(value: unknown, pathLabel: string, errors: ProtocolValidationIssue[]): void {
  validateExactObject(
    value,
    pathLabel,
    {
      required: ["handle"],
      optional: ["displayName"],
      properties: {
        handle: patternString(REMOTE_HANDLE_RE, "remote handle"),
        displayName: boundedString(1, 80),
      },
    },
    errors,
  );
}

function supportedProtocolVersions(
  value: unknown,
  pathLabel: string,
  errors: ProtocolValidationIssue[],
): void {
  if (!Array.isArray(value)) {
    errors.push(errorAt(pathLabel, "must be an array"));
    return;
  }

  if (value.length !== 1 || value[0] !== PROTOCOL_VERSION) {
    errors.push(errorAt(pathLabel, `must be exactly ["${PROTOCOL_VERSION}"]`));
  }
}

function arrayOf(itemValidator: Validator, maxItems: number): Validator {
  return (value, pathLabel, errors) => {
    if (!Array.isArray(value)) {
      errors.push(errorAt(pathLabel, "must be an array"));
      return;
    }

    if (value.length > maxItems) {
      errors.push(errorAt(pathLabel, `must contain at most ${maxItems} items`));
    }

    value.forEach((item, index) => itemValidator(item, `${pathLabel}[${index}]`, errors));
  };
}

function literal(expected: unknown): Validator {
  return (value, pathLabel, errors) => {
    if (value !== expected) {
      errors.push(errorAt(pathLabel, `must be ${JSON.stringify(expected)}`));
    }
  };
}

function enumValue(values: readonly unknown[]): Validator {
  return (value, pathLabel, errors) => {
    if (!values.includes(value)) {
      errors.push(errorAt(pathLabel, `must be one of ${values.join(", ")}`));
    }
  };
}

function boundedString(minLength: number, maxLength: number): Validator {
  return (value, pathLabel, errors) => {
    if (typeof value !== "string") {
      errors.push(errorAt(pathLabel, "must be a string"));
      return;
    }

    if (value.length < minLength || value.length > maxLength) {
      errors.push(errorAt(pathLabel, `must be ${minLength}-${maxLength} characters`));
    }
  };
}

function patternString(pattern: RegExp, label: string): Validator {
  return (value, pathLabel, errors) => {
    if (typeof value !== "string" || !pattern.test(value)) {
      errors.push(errorAt(pathLabel, `must be a valid ${label}`));
    }
  };
}

function booleanValue(
  value: unknown,
  pathLabel: string,
  errors: ProtocolValidationIssue[],
): void {
  if (typeof value !== "boolean") {
    errors.push(errorAt(pathLabel, "must be a boolean"));
  }
}

function integerAtLeast(minimum: number): Validator {
  return (value, pathLabel, errors) => {
    if (!Number.isInteger(value) || (value as number) < minimum) {
      errors.push(errorAt(pathLabel, `must be an integer >= ${minimum}`));
    }
  };
}

function timestamp(value: unknown, pathLabel: string, errors: ProtocolValidationIssue[]): void {
  if (typeof value !== "string" || Number.isNaN(Date.parse(value)) || !value.includes("T")) {
    errors.push(errorAt(pathLabel, "must be an ISO-8601 timestamp"));
  }
}

function base64AudioPayload(
  value: unknown,
  pathLabel: string,
  errors: ProtocolValidationIssue[],
): void {
  if (
    typeof value !== "string" ||
    value.length !== AUDIO_PAYLOAD_BASE64_CHARS ||
    !BASE64_RE.test(value)
  ) {
    errors.push(errorAt(pathLabel, "must be strict base64"));
    return;
  }

  const decoded = Buffer.from(value, "base64");
  if (decoded.byteLength !== AUDIO_FRAME_BYTES) {
    errors.push(errorAt(pathLabel, `must decode to ${AUDIO_FRAME_BYTES} bytes`));
  }
}

function collectForbiddenFieldNames(
  value: unknown,
  pathLabel = "$",
  found: string[] = [],
): string[] {
  if (Array.isArray(value)) {
    value.forEach((item, index) => collectForbiddenFieldNames(item, `${pathLabel}[${index}]`, found));
    return found;
  }

  if (!isPlainObject(value)) {
    return found;
  }

  for (const [key, child] of Object.entries(value)) {
    const childPath = `${pathLabel}.${key}`;
    if (FORBIDDEN_FIELD_NAME_RE.test(key)) {
      found.push(childPath);
    }
    collectForbiddenFieldNames(child, childPath, found);
  }

  return found;
}

function errorAt(pathLabel: string, message: string): ProtocolValidationIssue {
  return { path: pathLabel, message };
}

function asRecord(value: unknown): Record<string, unknown> {
  return isPlainObject(value) ? value : {};
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function hasOwn(value: Record<string, unknown>, key: string): boolean {
  return Object.prototype.hasOwnProperty.call(value, key);
}
