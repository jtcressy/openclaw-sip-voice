import { readdir, readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

export const PROTOCOL_VERSION = "1.0";
export const SUPPORTED_PROTOCOL_VERSIONS = [PROTOCOL_VERSION];
export const DEFAULT_BRIDGE_URL = "ws://127.0.0.1:9077";
export const UNSUPPORTED_VERSION_CLOSE_CODE = 1002;
export const AUDIO_FRAME_BYTES = 160;

const BASE64_RE = /^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/;
const CALL_ID_RE = /^call_[A-Za-z0-9._:-]{6,96}$/;
const COMMAND_ID_RE = /^cmd_[A-Za-z0-9._:-]{6,96}$/;
const BRIDGE_ID_RE = /^bridge_[A-Za-z0-9._:-]{3,96}$/;
const REASON_CODE_RE = /^[a-z][a-z0-9_]{1,63}$/;
const REMOTE_HANDLE_RE = /^[+A-Za-z0-9][A-Za-z0-9 ._()+-]{0,63}$/;
const FORBIDDEN_FIELD_NAME_RE =
  /(sip|sdp|rtp|unifi|gemini|openai|credential|secret|token|password|authorization|apikey|api_key|api-key)/i;

const AUDIO_FORMAT_SPEC = {
  required: [
    "format",
    "sampleRateHz",
    "channels",
    "frameDurationMs",
    "payloadEncoding"
  ],
  optional: [],
  properties: {
    format: literal("g711_ulaw"),
    sampleRateHz: literal(8000),
    channels: literal(1),
    frameDurationMs: literal(20),
    payloadEncoding: literal("base64")
  }
};

const AUDIO_FRAME_SPEC = {
  required: [...AUDIO_FORMAT_SPEC.required, "payload"],
  optional: [],
  properties: {
    ...AUDIO_FORMAT_SPEC.properties,
    payload: base64AudioPayload
  }
};

const MESSAGE_SPECS = {
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
      "capabilities"
    ],
    optional: [],
    properties: {
      protocolVersion: literal(PROTOCOL_VERSION),
      type: literal("hello"),
      sentAt: timestamp,
      bridgeId: patternString(BRIDGE_ID_RE, "bridge id"),
      bridgeVersion: boundedString(1, 40),
      transport: exactObject({
        required: ["kind", "defaultUrl"],
        optional: [],
        properties: {
          kind: literal("websocket"),
          defaultUrl: literal(DEFAULT_BRIDGE_URL)
        }
      }),
      supportedProtocolVersions: supportedProtocolVersions,
      audio: exactObject(AUDIO_FORMAT_SPEC),
      capabilities: exactObject({
        required: [
          "inboundCalls",
          "outboundCalls",
          "bargeIn",
          "clearQueuedAudio"
        ],
        optional: [],
        properties: {
          inboundCalls: booleanValue,
          outboundCalls: booleanValue,
          bargeIn: booleanValue,
          clearQueuedAudio: booleanValue
        }
      })
    }
  },
  status: {
    required: [
      "protocolVersion",
      "type",
      "sentAt",
      "bridgeState",
      "registration",
      "activeCalls"
    ],
    optional: [],
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
          message: boundedString(1, 160)
        }
      }),
      activeCalls: arrayOf(callSummary, 16)
    }
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
      "audio"
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
      requestedByCommandId: patternString(COMMAND_ID_RE, "command id")
    }
  },
  "audio.in": {
    required: [
      "protocolVersion",
      "type",
      "sentAt",
      "callId",
      "sequence",
      "timestampMs",
      "audio"
    ],
    optional: [],
    properties: {
      protocolVersion: literal(PROTOCOL_VERSION),
      type: literal("audio.in"),
      sentAt: timestamp,
      callId: patternString(CALL_ID_RE, "call id"),
      sequence: integerAtLeast(0),
      timestampMs: integerAtLeast(0),
      audio: exactObject(AUDIO_FRAME_SPEC)
    }
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
          message: boundedString(1, 160)
        }
      })
    },
    after(value, path, errors) {
      if (value.outcome === "error" && !hasOwn(value, "reason")) {
        errors.push(errorAt(`${path}.reason`, "reason is required when outcome is error"));
      }
    }
  },
  error: {
    required: ["protocolVersion", "type", "sentAt", "fatal", "error"],
    optional: [],
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
            "internal_error"
          ]),
          message: boundedString(1, 160),
          retryable: booleanValue,
          commandId: patternString(COMMAND_ID_RE, "command id"),
          callId: patternString(CALL_ID_RE, "call id"),
          expectedProtocolVersions: supportedProtocolVersions
        }
      })
    },
    after(value, path, errors) {
      if (value.error?.code !== "unsupported_protocol_version") {
        return;
      }

      if (value.fatal !== true) {
        errors.push(errorAt(`${path}.fatal`, "unsupported protocol version errors must be fatal"));
      }

      if (value.error.retryable !== false) {
        errors.push(errorAt(`${path}.error.retryable`, "unsupported protocol version errors are not retryable"));
      }

      if (!hasOwn(value.error, "expectedProtocolVersions")) {
        errors.push(errorAt(`${path}.error.expectedProtocolVersions`, "expectedProtocolVersions is required"));
      }
    }
  },
  "status.get": commandSpec("status.get", {}, []),
  "call.answer": commandSpec(
    "call.answer",
    { callId: patternString(CALL_ID_RE, "call id") },
    ["callId"]
  ),
  "call.dial": commandSpec(
    "call.dial",
    {
      remote: remoteParty,
      audio: exactObject(AUDIO_FORMAT_SPEC)
    },
    ["remote", "audio"]
  ),
  "audio.out": commandSpec(
    "audio.out",
    {
      callId: patternString(CALL_ID_RE, "call id"),
      sequence: integerAtLeast(0),
      audio: exactObject(AUDIO_FRAME_SPEC)
    },
    ["callId", "sequence", "audio"]
  ),
  "audio.clear": commandSpec(
    "audio.clear",
    {
      callId: patternString(CALL_ID_RE, "call id"),
      scope: literal("queued"),
      reason: enumValue(["barge_in", "user_request", "call_ending"])
    },
    ["callId", "scope"],
    ["reason"]
  ),
  "call.hangup": commandSpec(
    "call.hangup",
    {
      callId: patternString(CALL_ID_RE, "call id"),
      reason: enumValue(["user_request", "completed", "failed"])
    },
    ["callId"],
    ["reason"]
  )
};

const REQUIRED_VALID_FIXTURES = [
  "hello.valid.json",
  "status.registered.valid.json",
  "status.unregistered.valid.json",
  "call-start.inbound.valid.json",
  "call-start.outbound.valid.json",
  "audio-in.valid.json",
  "audio-out.valid.json",
  "audio-clear.valid.json",
  "call-end.completed.valid.json",
  "call-end.error.valid.json",
  "error.valid.json"
];

const REQUIRED_INVALID_FIXTURES = [
  "audio-in.missing-call-id.invalid.json",
  "audio-out.invalid-audio-format.invalid.json",
  "audio-in.invalid-base64-payload.invalid.json",
  "status.unexpected-sip-credential-field.invalid.json",
  "hello.unknown-protocol-version.invalid.json"
];

export function validateMessage(value) {
  if (!isPlainObject(value)) {
    return failure([errorAt("$", "message must be a JSON object")]);
  }

  if (typeof value.protocolVersion === "string" && value.protocolVersion !== PROTOCOL_VERSION) {
    return unsupportedProtocolVersionFailure(value.protocolVersion);
  }

  const errors = [];

  if (value.protocolVersion !== PROTOCOL_VERSION) {
    errors.push(errorAt("$.protocolVersion", `protocolVersion must be ${PROTOCOL_VERSION}`));
  }

  const forbiddenFields = collectForbiddenFieldNames(value);
  for (const fieldPath of forbiddenFields) {
    errors.push(errorAt(fieldPath, "field name is not allowed across the plugin boundary"));
  }

  if (typeof value.type !== "string") {
    errors.push(errorAt("$.type", "type must be a string"));
    return failure(errors);
  }

  const spec = MESSAGE_SPECS[value.type];
  if (!spec) {
    errors.push(errorAt("$.type", `unknown message type ${JSON.stringify(value.type)}`));
    return failure(errors);
  }

  validateExactObject(value, "$", spec, errors);

  if (errors.length > 0) {
    return failure(errors);
  }

  return { ok: true, code: null, errors: [] };
}

export function unsupportedProtocolVersionFailure(receivedVersion) {
  return {
    ok: false,
    code: "unsupported_protocol_version",
    fatal: true,
    closeCode: UNSUPPORTED_VERSION_CLOSE_CODE,
    expectedProtocolVersions: SUPPORTED_PROTOCOL_VERSIONS,
    errors: [
      errorAt(
        "$.protocolVersion",
        `unsupported protocol version ${JSON.stringify(receivedVersion)}`
      )
    ],
    event: buildUnsupportedProtocolVersionErrorEvent(receivedVersion)
  };
}

export function buildUnsupportedProtocolVersionErrorEvent(receivedVersion, sentAt = new Date().toISOString()) {
  return {
    protocolVersion: PROTOCOL_VERSION,
    type: "error",
    sentAt,
    fatal: true,
    error: {
      code: "unsupported_protocol_version",
      message: `Unsupported protocol version: ${String(receivedVersion)}`,
      retryable: false,
      expectedProtocolVersions: SUPPORTED_PROTOCOL_VERSIONS
    }
  };
}

export async function runFixtureValidation(protocolDir = currentProtocolDir()) {
  const failures = [];
  const schemaPath = path.join(protocolDir, "schemas", "message.schema.json");

  try {
    JSON.parse(await readFile(schemaPath, "utf8"));
  } catch (error) {
    failures.push({
      file: path.relative(protocolDir, schemaPath),
      errors: [`schema is not valid JSON: ${error.message}`]
    });
  }

  const validDir = path.join(protocolDir, "fixtures", "valid");
  const invalidDir = path.join(protocolDir, "fixtures", "invalid");
  const validFixtures = await jsonFiles(validDir);
  const invalidFixtures = await jsonFiles(invalidDir);

  requireFixtureNames("fixtures/valid", validFixtures, REQUIRED_VALID_FIXTURES, failures);
  requireFixtureNames("fixtures/invalid", invalidFixtures, REQUIRED_INVALID_FIXTURES, failures);

  let checked = 0;

  for (const fixture of validFixtures) {
    checked += 1;
    const fullPath = path.join(validDir, fixture);
    const message = await readJsonFixture(fullPath, protocolDir, failures);
    if (message === null) {
      continue;
    }

    const result = validateMessage(message);
    if (!result.ok) {
      failures.push({
        file: path.join("fixtures", "valid", fixture),
        errors: result.errors.map(formatValidationError)
      });
    }
  }

  for (const fixture of invalidFixtures) {
    checked += 1;
    const fullPath = path.join(invalidDir, fixture);
    const message = await readJsonFixture(fullPath, protocolDir, failures);
    if (message === null) {
      continue;
    }

    const result = validateMessage(message);
    if (result.ok) {
      failures.push({
        file: path.join("fixtures", "invalid", fixture),
        errors: ["fixture unexpectedly passed validation"]
      });
    }
  }

  return {
    ok: failures.length === 0,
    checked,
    failures
  };
}

function commandSpec(type, properties, requiredCommandFields, optionalCommandFields = []) {
  return {
    required: ["protocolVersion", "type", "sentAt", "commandId", ...requiredCommandFields],
    optional: optionalCommandFields,
    properties: {
      protocolVersion: literal(PROTOCOL_VERSION),
      type: literal(type),
      sentAt: timestamp,
      commandId: patternString(COMMAND_ID_RE, "command id"),
      ...properties
    }
  };
}

function validateExactObject(value, pathLabel, spec, errors) {
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
    if (validator) {
      validator(value[key], `${pathLabel}.${key}`, errors);
    }
  }

  spec.after?.(value, pathLabel, errors);
}

function exactObject(spec) {
  return (value, pathLabel, errors) => validateExactObject(value, pathLabel, spec, errors);
}

function callSummary(value, pathLabel, errors) {
  validateExactObject(
    value,
    pathLabel,
    {
      required: ["callId", "direction", "state"],
      optional: [],
      properties: {
        callId: patternString(CALL_ID_RE, "call id"),
        direction: enumValue(["inbound", "outbound"]),
        state: enumValue(["ringing", "dialing", "active", "ending"])
      }
    },
    errors
  );
}

function remoteParty(value, pathLabel, errors) {
  validateExactObject(
    value,
    pathLabel,
    {
      required: ["handle"],
      optional: ["displayName"],
      properties: {
        handle: remoteHandle,
        displayName: boundedString(1, 80)
      }
    },
    errors
  );
}

function remoteHandle(value, pathLabel, errors) {
  patternString(REMOTE_HANDLE_RE, "remote handle")(value, pathLabel, errors);
}

function supportedProtocolVersions(value, pathLabel, errors) {
  if (!Array.isArray(value)) {
    errors.push(errorAt(pathLabel, "must be an array"));
    return;
  }

  if (value.length !== 1 || value[0] !== PROTOCOL_VERSION) {
    errors.push(errorAt(pathLabel, `must be exactly ["${PROTOCOL_VERSION}"]`));
  }
}

function arrayOf(itemValidator, maxItems) {
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

function literal(expected) {
  return (value, pathLabel, errors) => {
    if (value !== expected) {
      errors.push(errorAt(pathLabel, `must be ${JSON.stringify(expected)}`));
    }
  };
}

function enumValue(values) {
  return (value, pathLabel, errors) => {
    if (!values.includes(value)) {
      errors.push(errorAt(pathLabel, `must be one of ${values.join(", ")}`));
    }
  };
}

function boundedString(minLength, maxLength) {
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

function patternString(pattern, label) {
  return (value, pathLabel, errors) => {
    if (typeof value !== "string" || !pattern.test(value)) {
      errors.push(errorAt(pathLabel, `must be a valid ${label}`));
    }
  };
}

function booleanValue(value, pathLabel, errors) {
  if (typeof value !== "boolean") {
    errors.push(errorAt(pathLabel, "must be a boolean"));
  }
}

function integerAtLeast(minimum) {
  return (value, pathLabel, errors) => {
    if (!Number.isInteger(value) || value < minimum) {
      errors.push(errorAt(pathLabel, `must be an integer >= ${minimum}`));
    }
  };
}

function timestamp(value, pathLabel, errors) {
  if (typeof value !== "string" || Number.isNaN(Date.parse(value)) || !value.includes("T")) {
    errors.push(errorAt(pathLabel, "must be an ISO-8601 timestamp"));
  }
}

function base64AudioPayload(value, pathLabel, errors) {
  if (typeof value !== "string" || !BASE64_RE.test(value)) {
    errors.push(errorAt(pathLabel, "must be strict base64"));
    return;
  }

  const decoded = Buffer.from(value, "base64");
  if (decoded.byteLength !== AUDIO_FRAME_BYTES) {
    errors.push(errorAt(pathLabel, `must decode to ${AUDIO_FRAME_BYTES} bytes`));
  }
}

function collectForbiddenFieldNames(value, pathLabel = "$", found = []) {
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

function failure(errors) {
  return {
    ok: false,
    code: "validation_failed",
    fatal: false,
    errors
  };
}

function errorAt(pathLabel, message) {
  return { path: pathLabel, message };
}

function formatValidationError(error) {
  return `${error.path}: ${error.message}`;
}

function isPlainObject(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function hasOwn(value, key) {
  return Object.prototype.hasOwnProperty.call(value, key);
}

async function jsonFiles(dir) {
  return (await readdir(dir)).filter((file) => file.endsWith(".json")).sort();
}

function requireFixtureNames(label, actual, required, failures) {
  const actualSet = new Set(actual);
  for (const name of required) {
    if (!actualSet.has(name)) {
      failures.push({
        file: label,
        errors: [`missing required fixture ${name}`]
      });
    }
  }
}

async function readJsonFixture(fullPath, protocolDir, failures) {
  try {
    return JSON.parse(await readFile(fullPath, "utf8"));
  } catch (error) {
    failures.push({
      file: path.relative(protocolDir, fullPath),
      errors: [`fixture is not valid JSON: ${error.message}`]
    });
    return null;
  }
}

function currentProtocolDir() {
  return path.dirname(fileURLToPath(import.meta.url));
}
