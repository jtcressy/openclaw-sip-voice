export const PROTOCOL_VERSION = "1.0" as const;
export const DEFAULT_BRIDGE_URL = "ws://127.0.0.1:9077" as const;

export type ProtocolVersion = typeof PROTOCOL_VERSION;
export type Direction = "inbound" | "outbound";

export interface AudioFormat {
  format: "g711_ulaw";
  sampleRateHz: 8000;
  channels: 1;
  frameDurationMs: 20;
  payloadEncoding: "base64";
}

export interface AudioFrame extends AudioFormat {
  payload: string;
}

export interface MessageEnvelope {
  protocolVersion: ProtocolVersion;
  type: string;
  sentAt: string;
}

export interface CommandEnvelope extends MessageEnvelope {
  commandId: string;
}

export interface RemoteParty {
  handle: string;
  displayName?: string;
}

export interface HelloEvent extends MessageEnvelope {
  type: "hello";
  bridgeId: string;
  bridgeVersion: string;
  transport: {
    kind: "websocket";
    defaultUrl: typeof DEFAULT_BRIDGE_URL;
  };
  supportedProtocolVersions: [ProtocolVersion];
  audio: AudioFormat;
  capabilities: {
    inboundCalls: boolean;
    outboundCalls: boolean;
    bargeIn: boolean;
    clearQueuedAudio: boolean;
  };
}

export interface StatusEvent extends MessageEnvelope {
  type: "status";
  bridgeState: "starting" | "ready" | "degraded" | "offline";
  registration: {
    state: "registered" | "unregistered" | "registering" | "error";
    label?: string;
    reasonCode?: string;
    message?: string;
  };
  activeCalls: Array<{
    callId: string;
    direction: Direction;
    state: "ringing" | "dialing" | "active" | "ending";
  }>;
}

export interface CallStartEvent extends MessageEnvelope {
  type: "call.start";
  callId: string;
  direction: Direction;
  state: "ringing" | "dialing" | "active";
  remote: RemoteParty;
  audio: AudioFormat;
  requestedByCommandId?: string;
}

export interface AudioInEvent extends MessageEnvelope {
  type: "audio.in";
  callId: string;
  sequence: number;
  timestampMs: number;
  audio: AudioFrame;
}

export interface CallEndEvent extends MessageEnvelope {
  type: "call.end";
  callId: string;
  outcome: "completed" | "error" | "missed" | "rejected" | "canceled";
  durationMs?: number;
  reason?: {
    code: string;
    message?: string;
  };
}

export interface ErrorEvent extends MessageEnvelope {
  type: "error";
  fatal: boolean;
  error: {
    code:
      | "unsupported_protocol_version"
      | "validation_failed"
      | "bridge_unavailable"
      | "call_not_found"
      | "call_rejected"
      | "audio_underrun"
      | "internal_error";
    message: string;
    retryable: boolean;
    commandId?: string;
    callId?: string;
    expectedProtocolVersions?: [ProtocolVersion];
  };
}

export type BridgeEvent =
  | HelloEvent
  | StatusEvent
  | CallStartEvent
  | AudioInEvent
  | CallEndEvent
  | ErrorEvent;

export interface StatusGetCommand extends CommandEnvelope {
  type: "status.get";
}

export interface CallAnswerCommand extends CommandEnvelope {
  type: "call.answer";
  callId: string;
}

export interface CallDialCommand extends CommandEnvelope {
  type: "call.dial";
  remote: RemoteParty;
  audio: AudioFormat;
}

export interface AudioOutCommand extends CommandEnvelope {
  type: "audio.out";
  callId: string;
  sequence: number;
  audio: AudioFrame;
}

export interface AudioClearCommand extends CommandEnvelope {
  type: "audio.clear";
  callId: string;
  scope: "queued";
  reason?: "barge_in" | "user_request" | "call_ending";
}

export interface CallHangupCommand extends CommandEnvelope {
  type: "call.hangup";
  callId: string;
  reason?: "user_request" | "completed" | "failed";
}

export type PluginCommand =
  | StatusGetCommand
  | CallAnswerCommand
  | CallDialCommand
  | AudioOutCommand
  | AudioClearCommand
  | CallHangupCommand;

export type ProtocolMessage = BridgeEvent | PluginCommand;
