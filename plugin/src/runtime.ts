import type {
  OpenClawConfig,
  PluginLogger,
  OpenClawPluginApi,
} from "openclaw/plugin-sdk/plugin-entry";
import {
  SipVoiceBridgeClient,
  type SipVoiceBridgeClientCommandOptions,
  type SipVoiceBridgeClientState,
  type SipVoiceBridgeConnectionState,
  type SipVoiceBridgeEventListener,
  type SipVoiceBridgeStateListener,
} from "./bridge-client.js";
import { summarizeSipVoiceConfig, type SipVoiceConfig } from "./config.js";
import {
  createSipVoiceRealtimeBridgeSession,
  resolveSipVoiceRealtimeAudioFormat,
  resolveSipVoiceRealtimeProvider,
} from "./realtime.js";
import {
  AUDIO_FRAME_BYTES,
  type AudioClearCommand,
  type AudioInEvent,
  type AudioOutCommand,
  type BridgeEvent,
  type BuildAudioClearCommandOptions,
  type BuildAudioOutCommandOptions,
  type BuildCallAnswerCommandOptions,
  type BuildCallDialCommandOptions,
  type BuildCallHangupCommandOptions,
  type CallDialCommand,
  type CallEndEvent,
  type CallHangupCommand,
  type CallStartEvent,
  type HelloEvent,
  type RemoteParty,
  type StatusEvent,
} from "./protocol.js";

type SipVoiceRealtimeProviders = NonNullable<
  Parameters<typeof resolveSipVoiceRealtimeProvider>[0]["providers"]
>;

export type SipVoiceRuntimeEventRecord = {
  at: string;
  type: string;
  callId?: string;
  message?: string;
  details?: Record<string, string | number | boolean | null>;
};

export type SipVoiceRuntimeErrorRecord = {
  at: string;
  message: string;
  callId?: string;
  code?: string;
  fatal?: boolean;
};

export type SipVoiceRuntimeCallStatus = {
  callId: string;
  direction: "inbound" | "outbound";
  state: "ringing" | "dialing" | "active" | "ending";
  remote: RemoteParty;
  startedAt: string;
  requestedByCommandId?: string;
};

export type SipVoiceRuntimeStatus = {
  enabled: boolean;
  implementation: "poc-call-manager";
  startedAt: string;
  stopped: boolean;
  bridgeConnected: boolean;
  bridge: {
    url: string;
    connectionState: SipVoiceBridgeConnectionState;
    hello: HelloEvent | null;
    lastStatus: StatusEvent | null;
    lastError: string | null;
  };
  activeCalls: number;
  pendingCalls: number;
  maxConcurrentCalls: number;
  calls: SipVoiceRuntimeCallStatus[];
  config: ReturnType<typeof summarizeSipVoiceConfig>;
  realtime: {
    audioFormat: ReturnType<typeof resolveSipVoiceRealtimeAudioFormat>;
    provider: string;
    model: string;
    resolvedProviderId?: string;
    providerConfigured: boolean;
    providerError?: string;
  };
  recentEvents: SipVoiceRuntimeEventRecord[];
  recentErrors: SipVoiceRuntimeErrorRecord[];
};

export type SipVoiceRuntimeTail = {
  events: SipVoiceRuntimeEventRecord[];
  errors: SipVoiceRuntimeErrorRecord[];
};

export type SipVoiceRuntimeOutboundCallResult = {
  commandId: string;
  remote: RemoteParty;
  messageQueued: boolean;
};

export type SipVoiceRuntimeSpeakResult = {
  callId: string;
  messageQueued: true;
};

export type SipVoiceRuntime = {
  status: () => SipVoiceRuntimeStatus;
  getStatus: () => SipVoiceRuntimeStatus;
  call: (
    target: string | RemoteParty,
    message?: string,
  ) => Promise<SipVoiceRuntimeOutboundCallResult>;
  speak: (callId: string, message: string) => Promise<SipVoiceRuntimeSpeakResult>;
  hangup: (callId: string) => Promise<CallHangupCommand>;
  tail: (limit?: number) => SipVoiceRuntimeTail;
  stop: () => Promise<void>;
};

export type SipVoiceRuntimeBridgeClient = {
  getState(): SipVoiceBridgeClientState;
  onEvent(listener: SipVoiceBridgeEventListener): () => void;
  onStateChange(listener: SipVoiceBridgeStateListener): () => void;
  connect(): Promise<void>;
  disconnect(code?: number, reason?: string): void;
  sendStatusGet(options?: SipVoiceBridgeClientCommandOptions): unknown;
  sendCallAnswer(options: BuildCallAnswerCommandOptions): unknown;
  sendCallDial(options: BuildCallDialCommandOptions): CallDialCommand;
  sendAudioOut(options: BuildAudioOutCommandOptions): AudioOutCommand;
  sendAudioClear(options: BuildAudioClearCommandOptions): AudioClearCommand;
  sendCallHangup(options: BuildCallHangupCommandOptions): CallHangupCommand;
};

export type SipVoiceRuntimeRealtimeSession = {
  connect?: () => unknown;
  sendAudio?: (audio: Buffer) => unknown;
  speak?: (message: string) => unknown;
  sendText?: (message: string) => unknown;
  close?: () => unknown;
  bridge?: {
    connect?: () => unknown;
    sendAudio?: (audio: Buffer) => unknown;
    speak?: (message: string) => unknown;
    sendText?: (message: string) => unknown;
    close?: () => unknown;
  };
};

type CreateRealtimeSessionParams = Parameters<
  typeof createSipVoiceRealtimeBridgeSession
>[0];

export type SipVoiceRuntimeRealtimeSessionFactory = (
  params: CreateRealtimeSessionParams,
) => SipVoiceRuntimeRealtimeSession;

export type CreateSipVoiceRuntimeParams = {
  config: SipVoiceConfig;
  fullConfig: OpenClawConfig;
  runtime: OpenClawPluginApi["runtime"];
  logger: PluginLogger;
  bridgeClient?: SipVoiceRuntimeBridgeClient;
  bridgeClientFactory?: (options: { url: string }) => SipVoiceRuntimeBridgeClient;
  createRealtimeSession?: SipVoiceRuntimeRealtimeSessionFactory;
  realtimeProviders?: SipVoiceRealtimeProviders;
  now?: () => Date;
};

type ManagedCallState = "ringing" | "dialing" | "active" | "ending" | "ended";

type ManagedCall = {
  callId: string;
  direction: "inbound" | "outbound";
  state: ManagedCallState;
  remote: RemoteParty;
  startedAt: string;
  endedAt?: string;
  requestedByCommandId?: string;
  outcome?: CallEndEvent["outcome"];
  endReason?: string;
  session?: SipVoiceRuntimeRealtimeSession;
  nextAudioOutSequence: number;
  outboundAudioRemainder: Buffer;
};

type PendingOutboundCall = {
  commandId: string;
  remote: RemoteParty;
  message?: string;
  startedAt: string;
};

const RECENT_RECORD_LIMIT = 50;
const DEFAULT_TAIL_LIMIT = 20;
const DEFAULT_MAX_CONCURRENT_CALLS = 1;
const SENSITIVE_KEY_RE =
  /(sip|sdp|rtp|unifi|gemini|openai|credential|secret|token|password|authorization|apikey|api_key|api-key)/i;
const SENSITIVE_TEXT_RE =
  /\b((?:sip|sdp|rtp|unifi|gemini|openai|credential|secret|token|password|authorization|apikey|api_key|api-key)[A-Za-z0-9_.-]*)(\s*[:=]\s*)([^,\s;]+)/gi;

function formatError(error: unknown): string {
  const message = error instanceof Error ? error.message : String(error);
  return redactSensitiveText(message);
}

function maxConcurrentCallsFromConfig(config: SipVoiceConfig): number {
  return Number.isInteger(config.maxConcurrentCalls) && config.maxConcurrentCalls > 0
    ? config.maxConcurrentCalls
    : DEFAULT_MAX_CONCURRENT_CALLS;
}

function normalizeRemoteParty(target: string | RemoteParty): RemoteParty {
  if (typeof target === "string") {
    return { handle: target };
  }
  return {
    handle: target.handle,
    ...(target.displayName === undefined ? {} : { displayName: target.displayName }),
  };
}

function isPromiseLike(value: unknown): value is PromiseLike<unknown> {
  return (
    value !== null &&
    (typeof value === "object" || typeof value === "function") &&
    "then" in value &&
    typeof (value as { then?: unknown }).then === "function"
  );
}

function pushRecent<T>(records: T[], record: T): void {
  records.push(record);
  if (records.length > RECENT_RECORD_LIMIT) {
    records.splice(0, records.length - RECENT_RECORD_LIMIT);
  }
}

function limitFromTailRequest(limit: number | undefined): number {
  if (!Number.isFinite(limit)) {
    return DEFAULT_TAIL_LIMIT;
  }
  return Math.max(0, Math.min(RECENT_RECORD_LIMIT, Math.trunc(limit)));
}

function redactSensitiveText(value: string): string {
  return value.replace(SENSITIVE_TEXT_RE, "$1$2[redacted]");
}

function sanitizeBridgeSnapshot<T>(value: T): T {
  if (value === null || value === undefined) {
    return value;
  }
  if (typeof value === "string") {
    return redactSensitiveText(value) as T;
  }
  if (Array.isArray(value)) {
    return value.map((item) => sanitizeBridgeSnapshot(item)) as T;
  }
  if (typeof value !== "object") {
    return value;
  }

  const sanitized: Record<string, unknown> = {};
  for (const [key, child] of Object.entries(value as Record<string, unknown>)) {
    sanitized[key] = SENSITIVE_KEY_RE.test(key)
      ? "[redacted]"
      : sanitizeBridgeSnapshot(child);
  }
  return sanitized as T;
}

function audioClearReasonFromValue(
  value: unknown,
): AudioClearCommand["reason"] | undefined {
  if (value === "barge_in" || value === "user_request" || value === "call_ending") {
    return value;
  }

  if (value && typeof value === "object" && "reason" in value) {
    return audioClearReasonFromValue((value as { reason?: unknown }).reason);
  }

  return "barge_in";
}

function callStatusFromRecord(record: ManagedCall): SipVoiceRuntimeCallStatus {
  return {
    callId: record.callId,
    direction: record.direction,
    state: record.state === "ended" ? "ending" : record.state,
    remote: record.remote,
    startedAt: record.startedAt,
    ...(record.requestedByCommandId === undefined
      ? {}
      : { requestedByCommandId: record.requestedByCommandId }),
  };
}

export async function createSipVoiceRuntime(
  params: CreateSipVoiceRuntimeParams,
): Promise<SipVoiceRuntime> {
  const now = params.now ?? (() => new Date());
  const startedAt = now().toISOString();
  const maxConcurrentCalls = maxConcurrentCallsFromConfig(params.config);
  const bridge =
    params.bridgeClient ??
    params.bridgeClientFactory?.({ url: params.config.bridge.url }) ??
    new SipVoiceBridgeClient({ url: params.config.bridge.url });
  const createRealtimeSession: SipVoiceRuntimeRealtimeSessionFactory =
    params.createRealtimeSession ??
    ((sessionParams) =>
      createSipVoiceRealtimeBridgeSession(sessionParams) as SipVoiceRuntimeRealtimeSession);

  let stopped = false;
  let latestBridgeState = bridge.getState();
  let helloSnapshot = latestBridgeState.hello;
  let statusSnapshot = latestBridgeState.lastStatus;

  const calls = new Map<string, ManagedCall>();
  const pendingOutboundCalls = new Map<string, PendingOutboundCall>();
  const recentEvents: SipVoiceRuntimeEventRecord[] = [];
  const recentErrors: SipVoiceRuntimeErrorRecord[] = [];

  const recordEvent = (
    type: string,
    params: {
      callId?: string;
      message?: string;
      details?: Record<string, string | number | boolean | null | undefined>;
    } = {},
  ): void => {
    const details =
      params.details === undefined
        ? undefined
        : Object.fromEntries(
            Object.entries(params.details)
              .filter(([, value]) => value !== undefined)
              .map(([key, value]) => [
                key,
                typeof value === "string" ? redactSensitiveText(value) : value,
              ]),
          );
    pushRecent(recentEvents, {
      at: now().toISOString(),
      type,
      ...(params.callId === undefined ? {} : { callId: params.callId }),
      ...(params.message === undefined
        ? {}
        : { message: redactSensitiveText(params.message) }),
      ...(details === undefined ? {} : { details }),
    });
  };

  const recordError = (
    error: unknown,
    context: {
      callId?: string;
      code?: string;
      fatal?: boolean;
      message?: string;
    } = {},
  ): Error => {
    const normalized = error instanceof Error ? error : new Error(String(error));
    const message = context.message
      ? `${redactSensitiveText(context.message)}: ${formatError(normalized)}`
      : formatError(normalized);
    pushRecent(recentErrors, {
      at: now().toISOString(),
      message,
      ...(context.callId === undefined ? {} : { callId: context.callId }),
      ...(context.code === undefined ? {} : { code: context.code }),
      ...(context.fatal === undefined ? {} : { fatal: context.fatal }),
    });
    params.logger.warn?.("[sip-voice] runtime error", {
      message,
      callId: context.callId,
      code: context.code,
    });
    return normalized;
  };

  const updateBridgeState = (state: SipVoiceBridgeClientState): void => {
    latestBridgeState = state;
    helloSnapshot = state.hello ?? helloSnapshot;
    statusSnapshot = state.lastStatus ?? statusSnapshot;
  };

  const activeCallCount = (): number => {
    let count = 0;
    for (const call of calls.values()) {
      if (call.state !== "ended") {
        count += 1;
      }
    }
    return count;
  };

  const reservedCallCount = (): number =>
    activeCallCount() + pendingOutboundCalls.size;

  const canAcceptAnotherCall = (): boolean =>
    reservedCallCount() < maxConcurrentCalls;

  const popOutboundAudioFrames = (call: ManagedCall, audio: Buffer): Buffer[] => {
    if (audio.byteLength === 0) {
      return [];
    }
    const pending =
      call.outboundAudioRemainder.byteLength === 0
        ? Buffer.from(audio)
        : Buffer.concat([call.outboundAudioRemainder, audio]);
    const frames: Buffer[] = [];
    let offset = 0;
    while (pending.byteLength - offset >= AUDIO_FRAME_BYTES) {
      frames.push(Buffer.from(pending.subarray(offset, offset + AUDIO_FRAME_BYTES)));
      offset += AUDIO_FRAME_BYTES;
    }
    call.outboundAudioRemainder =
      offset < pending.byteLength ? Buffer.from(pending.subarray(offset)) : Buffer.alloc(0);
    return frames;
  };

  const clearOutboundAudioBuffers = (call: ManagedCall): void => {
    call.outboundAudioRemainder = Buffer.alloc(0);
  };

  const sendAudioClear = (
    callId: string,
    reason?: AudioClearCommand["reason"],
  ): AudioClearCommand | null => {
    const call = calls.get(callId);
    if (call) {
      clearOutboundAudioBuffers(call);
    }
    try {
      const command = bridge.sendAudioClear({
        callId,
        ...(reason === undefined ? {} : { reason }),
      });
      recordEvent("audio.clear", {
        callId,
        details: reason === undefined ? undefined : { reason },
      });
      return command;
    } catch (error) {
      recordError(error, { callId, code: "audio_clear_failed" });
      return null;
    }
  };

  const createAudioSink = (callId: string) => {
    const sink = {
      sendAudio(audio: Uint8Array | Buffer): void {
        const call = calls.get(callId);
        if (!call || call.state === "ended") {
          return;
        }

        try {
          const frames = popOutboundAudioFrames(call, Buffer.from(audio));
          for (const frame of frames) {
            const command = bridge.sendAudioOut({
              callId,
              sequence: call.nextAudioOutSequence,
              payload: frame,
            });
            call.nextAudioOutSequence += 1;
            recordEvent("audio.out", {
              callId,
              details: {
                sequence: command.sequence,
                bytes: frame.byteLength,
              },
            });
          }
        } catch (error) {
          recordError(error, { callId, code: "audio_out_failed" });
        }
      },
      clear(reason?: unknown): void {
        sendAudioClear(callId, audioClearReasonFromValue(reason));
      },
      clearAudio(reason?: unknown): void {
        sendAudioClear(callId, audioClearReasonFromValue(reason));
      },
      sendClear(reason?: unknown): void {
        sendAudioClear(callId, audioClearReasonFromValue(reason));
      },
      sendMark(markName: string): void {
        recordEvent("audio.mark", { callId, details: { markName } });
      },
    };
    return sink;
  };

  const closeSession = (
    call: ManagedCall,
    reason: AudioClearCommand["reason"] = "call_ending",
  ): Promise<void> | void => {
    sendAudioClear(call.callId, reason);

    const session = call.session;
    if (!session) {
      return;
    }

    const close = session.close ?? session.bridge?.close;
    if (!close) {
      call.session = undefined;
      return;
    }

    try {
      const result = close.call(session.close ? session : session.bridge);
      call.session = undefined;
      if (isPromiseLike(result)) {
        return result.then(
          () => undefined,
          (error) => {
            recordError(error, { callId: call.callId, code: "session_close_failed" });
          },
        );
      }
    } catch (error) {
      recordError(error, { callId: call.callId, code: "session_close_failed" });
      call.session = undefined;
    }
  };

  const finalizeCall = (call: ManagedCall, event: CallEndEvent): void => {
    call.state = "ended";
    call.endedAt = now().toISOString();
    call.outcome = event.outcome;
    call.endReason = event.reason?.code;
    calls.delete(call.callId);
    recordEvent("call.end", {
      callId: call.callId,
      details: {
        outcome: event.outcome,
        durationMs: event.durationMs,
        reasonCode: event.reason?.code,
      },
    });
  };

  const finishCallAfterSessionClose = (call: ManagedCall, event: CallEndEvent): void => {
    const closeResult = closeSession(call, "call_ending");
    if (isPromiseLike(closeResult)) {
      closeResult.then(() => finalizeCall(call, event));
      return;
    }
    finalizeCall(call, event);
  };

  const createSessionForCall = (call: ManagedCall): boolean => {
    try {
      call.session = createRealtimeSession({
        config: params.config,
        fullConfig: params.fullConfig,
        providers: params.realtimeProviders,
        audioSink: createAudioSink(call.callId),
      });
      recordEvent("realtime.session.start", {
        callId: call.callId,
        details: {
          provider: params.config.realtime.provider,
          model: params.config.realtime.model,
        },
      });
      connectRealtimeSession(call);
      return true;
    } catch (error) {
      recordError(error, { callId: call.callId, code: "session_start_failed" });
      try {
        bridge.sendCallHangup({ callId: call.callId, reason: "failed" });
      } catch (hangupError) {
        recordError(hangupError, { callId: call.callId, code: "hangup_failed" });
      }
      call.state = "ended";
      calls.delete(call.callId);
      return false;
    }
  };

  const connectRealtimeSession = (call: ManagedCall): void => {
    const session = call.session;
    const connect = session?.connect ?? session?.bridge?.connect;
    if (!session || !connect) {
      recordError(new Error("Realtime session does not expose connect()"), {
        callId: call.callId,
        code: "realtime_contract_missing",
      });
      return;
    }

    const target = session.connect ? session : session.bridge;
    const handleConnected = (): void => {
      if (calls.get(call.callId) === call && call.state !== "ended") {
        recordEvent("realtime.session.ready", { callId: call.callId });
      }
    };
    const handleConnectionFailure = (error: unknown): void => {
      recordError(error, { callId: call.callId, code: "session_connect_failed" });
      if (calls.get(call.callId) !== call || call.state === "ended") {
        return;
      }
      try {
        bridge.sendCallHangup({ callId: call.callId, reason: "failed" });
      } catch (hangupError) {
        recordError(hangupError, { callId: call.callId, code: "hangup_failed" });
      }
      call.state = "ending";
      closeSession(call, "session_connect_failed");
    };

    try {
      const result = connect.call(target);
      recordEvent("realtime.session.connect", { callId: call.callId });
      if (isPromiseLike(result)) {
        result.then(handleConnected, handleConnectionFailure);
        return;
      }
      handleConnected();
    } catch (error) {
      handleConnectionFailure(error);
    }
  };

  const sendAudioToRealtime = (event: AudioInEvent): void => {
    const call = calls.get(event.callId);
    if (!call || call.state === "ended") {
      recordError(new Error("audio.in for unknown call"), {
        callId: event.callId,
        code: "call_not_found",
      });
      return;
    }

    const session = call.session;
    const sendAudio = session?.sendAudio ?? session?.bridge?.sendAudio;
    if (!session || !sendAudio) {
      recordError(new Error("Realtime session does not expose sendAudio(Buffer)"), {
        callId: event.callId,
        code: "realtime_contract_missing",
      });
      return;
    }

    const audio = Buffer.from(event.audio.payload, "base64");
    try {
      const target = session.sendAudio ? session : session.bridge;
      const result = sendAudio.call(target, audio);
      recordEvent("audio.in", {
        callId: event.callId,
        details: {
          sequence: event.sequence,
          bytes: audio.byteLength,
        },
      });
      if (isPromiseLike(result)) {
        result.catch((error) => {
          recordError(error, { callId: event.callId, code: "audio_in_failed" });
        });
      }
    } catch (error) {
      recordError(error, { callId: event.callId, code: "audio_in_failed" });
    }
  };

  const sendRealtimeSpeech = async (
    call: ManagedCall,
    message: string,
  ): Promise<void> => {
    const session = call.session;
    const speak = session?.speak ?? session?.bridge?.speak;
    const sendText = session?.sendText ?? session?.bridge?.sendText;
    const sender = speak ?? sendText;
    if (!session || !sender) {
      throw new Error("Realtime session does not expose speak(message)");
    }

    const target = speak
      ? session.speak
        ? session
        : session.bridge
      : session.sendText
        ? session
        : session.bridge;

    await sender.call(target, message);
    recordEvent("realtime.speak", { callId: call.callId });
  };

  const handleInboundCallStart = (event: CallStartEvent): void => {
    if (!canAcceptAnotherCall()) {
      recordError(new Error("maxConcurrentCalls reached"), {
        callId: event.callId,
        code: "call_rejected",
      });
      try {
        bridge.sendCallHangup({ callId: event.callId, reason: "failed" });
        recordEvent("call.reject", {
          callId: event.callId,
          details: { maxConcurrentCalls },
        });
      } catch (error) {
        recordError(error, { callId: event.callId, code: "hangup_failed" });
      }
      return;
    }

    const call: ManagedCall = {
      callId: event.callId,
      direction: "inbound",
      state: "active",
      remote: event.remote,
      startedAt: event.sentAt,
      nextAudioOutSequence: 0,
      outboundAudioRemainder: Buffer.alloc(0),
    };
    calls.set(event.callId, call);

    try {
      bridge.sendCallAnswer({ callId: event.callId });
      recordEvent("call.answer", { callId: event.callId });
    } catch (error) {
      recordError(error, { callId: event.callId, code: "answer_failed" });
      calls.delete(event.callId);
      return;
    }

    createSessionForCall(call);
  };

  const handleOutboundCallStart = (event: CallStartEvent): void => {
    const pending =
      event.requestedByCommandId === undefined
        ? undefined
        : pendingOutboundCalls.get(event.requestedByCommandId);
    if (event.requestedByCommandId !== undefined) {
      pendingOutboundCalls.delete(event.requestedByCommandId);
    }

    if (!pending && !canAcceptAnotherCall()) {
      recordError(new Error("maxConcurrentCalls reached"), {
        callId: event.callId,
        code: "call_rejected",
      });
      try {
        bridge.sendCallHangup({ callId: event.callId, reason: "failed" });
        recordEvent("call.reject", {
          callId: event.callId,
          details: { maxConcurrentCalls },
        });
      } catch (error) {
        recordError(error, { callId: event.callId, code: "hangup_failed" });
      }
      return;
    }

    const call: ManagedCall = {
      callId: event.callId,
      direction: "outbound",
      state: event.state,
      remote: event.remote,
      startedAt: event.sentAt,
      requestedByCommandId: event.requestedByCommandId,
      nextAudioOutSequence: 0,
      outboundAudioRemainder: Buffer.alloc(0),
    };
    calls.set(event.callId, call);

    if (!createSessionForCall(call) || pending?.message === undefined) {
      return;
    }

    sendRealtimeSpeech(call, pending.message).catch((error) => {
      recordError(error, { callId: call.callId, code: "speak_failed" });
    });
  };

  const handleCallStart = (event: CallStartEvent): void => {
    recordEvent("call.start", {
      callId: event.callId,
      details: {
        direction: event.direction,
        state: event.state,
        requestedByCommandId: event.requestedByCommandId,
      },
    });

    if (calls.has(event.callId)) {
      const existing = calls.get(event.callId);
      if (existing) {
        existing.state = event.state;
      }
      return;
    }

    if (event.direction === "inbound") {
      handleInboundCallStart(event);
      return;
    }

    handleOutboundCallStart(event);
  };

  const handleCallEnd = (event: CallEndEvent): void => {
    const call = calls.get(event.callId);
    if (!call) {
      for (const [commandId, pending] of pendingOutboundCalls) {
        if (pending.commandId === event.callId) {
          pendingOutboundCalls.delete(commandId);
        }
      }
      recordEvent("call.end", {
        callId: event.callId,
        details: { outcome: event.outcome, durationMs: event.durationMs },
      });
      return;
    }

    call.state = "ending";
    finishCallAfterSessionClose(call, event);
  };

  const handleBridgeEvent = (event: BridgeEvent): void => {
    if (event.type === "hello") {
      helloSnapshot = event;
      recordEvent("bridge.hello", {
        details: {
          bridgeId: event.bridgeId,
          bridgeVersion: event.bridgeVersion,
        },
      });
      return;
    }

    if (event.type === "status") {
      statusSnapshot = event;
      recordEvent("bridge.status", {
        details: {
          bridgeState: event.bridgeState,
          registration: event.registration.state,
          bridgeActiveCalls: event.activeCalls.length,
        },
      });
      return;
    }

    if (event.type === "error") {
      if (event.error.commandId !== undefined) {
        pendingOutboundCalls.delete(event.error.commandId);
      }
      recordError(new Error(event.error.message), {
        callId: event.error.callId,
        code: event.error.code,
        fatal: event.fatal,
      });
      return;
    }

    if (event.type === "call.start") {
      handleCallStart(event);
      return;
    }

    if (event.type === "audio.in") {
      sendAudioToRealtime(event);
      return;
    }

    handleCallEnd(event);
  };

  const detachEventListener = bridge.onEvent(handleBridgeEvent);
  const detachStateListener = bridge.onStateChange((state) => {
    updateBridgeState(state);
    recordEvent("bridge.state", {
      details: { connectionState: state.connectionState },
    });
  });

  const getStatus = (): SipVoiceRuntimeStatus => {
    updateBridgeState(bridge.getState());

    const realtime = {
      audioFormat: resolveSipVoiceRealtimeAudioFormat(params.config),
      provider: params.config.realtime.provider,
      model: params.config.realtime.model,
      providerConfigured: false,
    } satisfies SipVoiceRuntimeStatus["realtime"];

    try {
      const resolved = resolveSipVoiceRealtimeProvider({
        config: params.config,
        fullConfig: params.fullConfig,
        providers: params.realtimeProviders,
      });
      return {
        enabled: params.config.enabled,
        implementation: "poc-call-manager",
        startedAt,
        stopped,
        bridgeConnected: latestBridgeState.connectionState === "connected",
        bridge: {
          url: params.config.bridge.url,
          connectionState: latestBridgeState.connectionState,
          hello: sanitizeBridgeSnapshot(helloSnapshot),
          lastStatus: sanitizeBridgeSnapshot(statusSnapshot),
          lastError:
            latestBridgeState.lastError === null
              ? null
              : formatError(latestBridgeState.lastError),
        },
        activeCalls: activeCallCount(),
        pendingCalls: pendingOutboundCalls.size,
        maxConcurrentCalls,
        calls: Array.from(calls.values())
          .filter((call) => call.state !== "ended")
          .map(callStatusFromRecord),
        config: summarizeSipVoiceConfig(params.config),
        realtime: {
          ...realtime,
          resolvedProviderId: resolved.provider.id,
          providerConfigured: true,
        },
        recentEvents: [...recentEvents],
        recentErrors: [...recentErrors],
      };
    } catch (error) {
      return {
        enabled: params.config.enabled,
        implementation: "poc-call-manager",
        startedAt,
        stopped,
        bridgeConnected: latestBridgeState.connectionState === "connected",
        bridge: {
          url: params.config.bridge.url,
          connectionState: latestBridgeState.connectionState,
          hello: sanitizeBridgeSnapshot(helloSnapshot),
          lastStatus: sanitizeBridgeSnapshot(statusSnapshot),
          lastError:
            latestBridgeState.lastError === null
              ? null
              : formatError(latestBridgeState.lastError),
        },
        activeCalls: activeCallCount(),
        pendingCalls: pendingOutboundCalls.size,
        maxConcurrentCalls,
        calls: Array.from(calls.values())
          .filter((call) => call.state !== "ended")
          .map(callStatusFromRecord),
        config: summarizeSipVoiceConfig(params.config),
        realtime: {
          ...realtime,
          providerError: formatError(error),
        },
        recentEvents: [...recentEvents],
        recentErrors: [...recentErrors],
      };
    }
  };

  const runtime: SipVoiceRuntime = {
    status: getStatus,
    getStatus,
    async call(target, message) {
      if (stopped) {
        throw recordError(new Error("SIP voice runtime is stopped"), {
          code: "runtime_stopped",
        });
      }
      if (!params.config.enabled) {
        throw recordError(new Error("SIP voice runtime is disabled"), {
          code: "runtime_disabled",
        });
      }
      if (!canAcceptAnotherCall()) {
        throw recordError(new Error("maxConcurrentCalls reached"), {
          code: "call_rejected",
        });
      }

      const remote = normalizeRemoteParty(target);
      let command: CallDialCommand;
      try {
        command = bridge.sendCallDial({ remote });
      } catch (error) {
        throw recordError(error, { code: "dial_failed" });
      }

      pendingOutboundCalls.set(command.commandId, {
        commandId: command.commandId,
        remote,
        ...(message === undefined ? {} : { message }),
        startedAt: now().toISOString(),
      });
      recordEvent("call.dial", {
        details: {
          commandId: command.commandId,
          target: remote.handle,
          hasMessage: message !== undefined,
        },
      });
      return {
        commandId: command.commandId,
        remote,
        messageQueued: message !== undefined,
      };
    },
    async speak(callId, message) {
      const call = calls.get(callId);
      if (!call || call.state === "ended") {
        throw recordError(new Error(`Call not found: ${callId}`), {
          callId,
          code: "call_not_found",
        });
      }

      try {
        await sendRealtimeSpeech(call, message);
        return { callId, messageQueued: true };
      } catch (error) {
        throw recordError(error, { callId, code: "speak_failed" });
      }
    },
    async hangup(callId) {
      const call = calls.get(callId);
      if (!call || call.state === "ended") {
        throw recordError(new Error(`Call not found: ${callId}`), {
          callId,
          code: "call_not_found",
        });
      }

      let command: CallHangupCommand;
      try {
        command = bridge.sendCallHangup({ callId, reason: "user_request" });
      } catch (error) {
        throw recordError(error, { callId, code: "hangup_failed" });
      }

      call.state = "ending";
      const closeResult = closeSession(call, "user_request");
      if (isPromiseLike(closeResult)) {
        await closeResult;
      }
      recordEvent("call.hangup", { callId });
      return command;
    },
    tail(limit) {
      const normalized = limitFromTailRequest(limit);
      return {
        events: normalized === 0 ? [] : recentEvents.slice(-normalized),
        errors: normalized === 0 ? [] : recentErrors.slice(-normalized),
      };
    },
    async stop() {
      if (stopped) {
        return;
      }
      stopped = true;
      recordEvent("runtime.stop");

      detachEventListener();
      detachStateListener();

      const closeResults: Array<PromiseLike<unknown>> = [];
      for (const call of calls.values()) {
        call.state = "ending";
        const result = closeSession(call, "call_ending");
        if (isPromiseLike(result)) {
          closeResults.push(result);
        }
      }
      calls.clear();
      pendingOutboundCalls.clear();

      try {
        bridge.disconnect(1000, "sip voice runtime stopped");
      } catch (error) {
        recordError(error, { code: "bridge_disconnect_failed" });
      }

      await Promise.all(closeResults);
    },
  };

  params.logger.debug?.("[sip-voice] POC runtime initialized", {
    bridgeUrl: params.config.bridge.url,
    maxConcurrentCalls,
    provider: params.config.realtime.provider,
    model: params.config.realtime.model,
  });

  if (params.config.enabled) {
    try {
      await bridge.connect();
      updateBridgeState(bridge.getState());
      recordEvent("bridge.connect");
      try {
        bridge.sendStatusGet();
      } catch (error) {
        recordError(error, { code: "status_get_failed" });
      }
    } catch (error) {
      updateBridgeState(bridge.getState());
      recordError(error, { code: "bridge_connect_failed" });
    }
  } else {
    recordEvent("runtime.disabled");
  }

  return runtime;
}
