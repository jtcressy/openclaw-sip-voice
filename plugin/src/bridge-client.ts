import {
  buildAudioClearCommand,
  buildAudioOutCommand,
  buildCallAnswerCommand,
  buildCallDialCommand,
  buildCallHangupCommand,
  buildStatusGetCommand,
  createCommandId,
  DEFAULT_BRIDGE_URL,
  parseBridgeEvent,
  ProtocolValidationError,
  UNSUPPORTED_VERSION_CLOSE_CODE,
} from "./protocol.js";
import type {
  AudioClearCommand,
  AudioFrame,
  AudioOutCommand,
  BridgeEvent,
  BuildAudioClearCommandOptions,
  BuildAudioOutCommandOptions,
  BuildCallAnswerCommandOptions,
  BuildCallDialCommandOptions,
  BuildCallHangupCommandOptions,
  BuildCommandOptions,
  CallAnswerCommand,
  CallDialCommand,
  CallHangupCommand,
  ErrorEvent,
  HelloEvent,
  PluginCommand,
  StatusEvent,
  StatusGetCommand,
} from "./protocol.js";

export type SipVoiceBridgeConnectionState =
  | "disconnected"
  | "connecting"
  | "connected";

export type SipVoiceBridgeClientState = {
  connectionState: SipVoiceBridgeConnectionState;
  lastError: Error | ProtocolValidationError | null;
  lastStatus: StatusEvent | null;
  hello: HelloEvent | null;
};

export type SipVoiceBridgeTransportEventType =
  | "open"
  | "message"
  | "error"
  | "close";

export type SipVoiceBridgeTransportListener = (event: unknown) => void;

export type SipVoiceBridgeTransport = {
  readyState?: number;
  send(data: string): void;
  close(code?: number, reason?: string): void;
  addEventListener?: (
    type: SipVoiceBridgeTransportEventType,
    listener: SipVoiceBridgeTransportListener,
  ) => void;
  removeEventListener?: (
    type: SipVoiceBridgeTransportEventType,
    listener: SipVoiceBridgeTransportListener,
  ) => void;
  onopen?: SipVoiceBridgeTransportListener | null;
  onmessage?: SipVoiceBridgeTransportListener | null;
  onerror?: SipVoiceBridgeTransportListener | null;
  onclose?: SipVoiceBridgeTransportListener | null;
};

export type SipVoiceBridgeTransportFactory = (url: string) => SipVoiceBridgeTransport;

export type SipVoiceBridgeClientOptions = {
  url?: string;
  transportFactory?: SipVoiceBridgeTransportFactory;
  now?: () => Date;
  commandIdFactory?: () => string;
};

export type SipVoiceBridgeClientCommandOptions = BuildCommandOptions;
export type SipVoiceBridgeEventListener = (event: BridgeEvent) => void;
export type SipVoiceBridgeStateListener = (state: SipVoiceBridgeClientState) => void;

type PendingConnect = {
  resolve: () => void;
  reject: (error: Error) => void;
};

const TRANSPORT_OPEN = 1;

export class SipVoiceBridgeClient {
  readonly url: string;

  private readonly transportFactory: SipVoiceBridgeTransportFactory;
  private readonly now: () => Date;
  private readonly commandIdFactory: () => string;
  private readonly eventListeners = new Set<SipVoiceBridgeEventListener>();
  private readonly stateListeners = new Set<SipVoiceBridgeStateListener>();

  private transport: SipVoiceBridgeTransport | null = null;
  private detachTransportListeners: (() => void) | null = null;
  private pendingConnect: PendingConnect | null = null;
  private connectPromise: Promise<void> | null = null;
  private connectionState: SipVoiceBridgeConnectionState = "disconnected";
  private lastError: Error | ProtocolValidationError | null = null;
  private lastStatus: StatusEvent | null = null;
  private hello: HelloEvent | null = null;

  constructor(options: SipVoiceBridgeClientOptions = {}) {
    this.url = options.url ?? DEFAULT_BRIDGE_URL;
    this.transportFactory = options.transportFactory ?? createDefaultTransport;
    this.now = options.now ?? (() => new Date());
    this.commandIdFactory = options.commandIdFactory ?? createCommandId;
  }

  getState(): SipVoiceBridgeClientState {
    return {
      connectionState: this.connectionState,
      lastError: this.lastError,
      lastStatus: this.lastStatus,
      hello: this.hello,
    };
  }

  onEvent(listener: SipVoiceBridgeEventListener): () => void {
    this.eventListeners.add(listener);
    return () => {
      this.eventListeners.delete(listener);
    };
  }

  onStateChange(listener: SipVoiceBridgeStateListener): () => void {
    this.stateListeners.add(listener);
    return () => {
      this.stateListeners.delete(listener);
    };
  }

  ["connect"](): Promise<void> {
    if (this.connectionState === "connected") {
      return Promise.resolve();
    }
    if (this.connectionState === "connecting" && this.connectPromise) {
      return this.connectPromise;
    }

    this.setConnectionState("connecting");

    try {
      const transport = this.transportFactory(this.url);
      this.transport = transport;
      this.detachTransportListeners = attachTransportListeners(transport, {
        open: (event) => this.handleTransportOpen(event),
        message: (event) => this.handleTransportMessage(event),
        error: (event) => this.handleTransportError(event),
        close: (event) => this.handleTransportClose(event),
      });

      this.connectPromise = new Promise<void>((resolve, reject) => {
        this.pendingConnect = { resolve, reject };
      });

      if (transport.readyState === TRANSPORT_OPEN) {
        queueMicrotask(() => this.handleTransportOpen({}));
      }

      return this.connectPromise;
    } catch (error) {
      const normalized = toError(error, "Failed to create SIP voice bridge transport");
      this.lastError = normalized;
      this.finishDisconnected(normalized);
      return Promise.reject(normalized);
    }
  }

  disconnect(code = 1000, reason = "client disconnect"): void {
    this.closeTransport(code, reason);
  }

  sendStatusGet(options: SipVoiceBridgeClientCommandOptions = {}): StatusGetCommand {
    return this.sendCommand(buildStatusGetCommand(this.withCommandOptions(options)));
  }

  sendCallAnswer(options: BuildCallAnswerCommandOptions): CallAnswerCommand {
    return this.sendCommand(buildCallAnswerCommand(this.withCommandOptions(options)));
  }

  sendCallDial(options: BuildCallDialCommandOptions): CallDialCommand {
    return this.sendCommand(buildCallDialCommand(this.withCommandOptions(options)));
  }

  sendAudioOut(options: BuildAudioOutCommandOptions): AudioOutCommand {
    return this.sendCommand(buildAudioOutCommand(this.withCommandOptions(options)));
  }

  sendAudioClear(options: BuildAudioClearCommandOptions): AudioClearCommand {
    return this.sendCommand(buildAudioClearCommand(this.withCommandOptions(options)));
  }

  sendCallHangup(options: BuildCallHangupCommandOptions): CallHangupCommand {
    return this.sendCommand(buildCallHangupCommand(this.withCommandOptions(options)));
  }

  private sendCommand<TCommand extends PluginCommand>(command: TCommand): TCommand {
    if (this.connectionState !== "connected" || !this.transport) {
      throw new Error("SIP voice bridge is not connected");
    }

    this.transport.send(JSON.stringify(command));
    return command;
  }

  private withCommandOptions<TOptions extends BuildCommandOptions>(
    options: TOptions,
  ): TOptions & Required<BuildCommandOptions> {
    return {
      ...options,
      commandId: options.commandId ?? this.commandIdFactory(),
      sentAt: options.sentAt ?? this.now().toISOString(),
    };
  }

  private handleTransportOpen(_event: unknown): void {
    this.resolvePendingConnect();
    this.lastError = null;
    this.setConnectionState("connected");
  }

  private handleTransportMessage(event: unknown): void {
    try {
      const bridgeEvent = parseBridgeEvent(readTransportMessageData(event));
      this.applyBridgeEvent(bridgeEvent);
      this.emitEvent(bridgeEvent);
    } catch (error) {
      const normalized = toError(error, "Invalid SIP voice bridge event");
      this.lastError = normalized;
      this.emitState();

      if (
        normalized instanceof ProtocolValidationError &&
        normalized.code === "unsupported_protocol_version"
      ) {
        this.closeTransport(UNSUPPORTED_VERSION_CLOSE_CODE, "unsupported protocol version");
      }
    }
  }

  private handleTransportError(event: unknown): void {
    const error = toError(event, "SIP voice bridge transport error");
    this.lastError = error;
    this.rejectPendingConnect(error);
    this.emitState();
  }

  private handleTransportClose(_event: unknown): void {
    this.finishDisconnected();
  }

  private applyBridgeEvent(event: BridgeEvent): void {
    if (event.type === "hello") {
      this.hello = event;
      this.emitState();
      return;
    }

    if (event.type === "status") {
      this.lastStatus = event;
      this.emitState();
      return;
    }

    if (event.type === "error") {
      this.lastError = bridgeErrorEventToError(event);
      this.emitState();
    }
  }

  private setConnectionState(connectionState: SipVoiceBridgeConnectionState): void {
    if (this.connectionState === connectionState) {
      return;
    }
    this.connectionState = connectionState;
    this.emitState();
  }

  private closeTransport(code: number, reason?: string): void {
    const transport = this.transport;
    if (!transport) {
      this.finishDisconnected();
      return;
    }

    try {
      transport.close(code, reason);
    } finally {
      this.finishDisconnected();
    }
  }

  private finishDisconnected(error?: Error): void {
    this.detachTransportListeners?.();
    this.detachTransportListeners = null;
    this.transport = null;

    if (error) {
      this.lastError = error;
    }
    this.rejectPendingConnect(error ?? new Error("SIP voice bridge disconnected"));
    this.connectPromise = null;
    this.setConnectionState("disconnected");
  }

  private resolvePendingConnect(): void {
    this.pendingConnect?.resolve();
    this.pendingConnect = null;
    this.connectPromise = null;
  }

  private rejectPendingConnect(error: Error): void {
    this.pendingConnect?.reject(error);
    this.pendingConnect = null;
    this.connectPromise = null;
  }

  private emitEvent(event: BridgeEvent): void {
    for (const listener of this.eventListeners) {
      listener(event);
    }
  }

  private emitState(): void {
    const state = this.getState();
    for (const listener of this.stateListeners) {
      listener(state);
    }
  }
}

function createDefaultTransport(url: string): SipVoiceBridgeTransport {
  const globalWithWebSocket = globalThis as typeof globalThis & {
    WebSocket?: new (url: string) => SipVoiceBridgeTransport;
  };
  if (typeof globalWithWebSocket.WebSocket !== "function") {
    throw new Error("No WebSocket implementation available; pass transportFactory");
  }
  return new globalWithWebSocket.WebSocket(url);
}

function attachTransportListeners(
  transport: SipVoiceBridgeTransport,
  listeners: Record<SipVoiceBridgeTransportEventType, SipVoiceBridgeTransportListener>,
): () => void {
  const detach = Object.entries(listeners).map(([type, listener]) =>
    attachTransportListener(
      transport,
      type as SipVoiceBridgeTransportEventType,
      listener,
    ),
  );

  return () => {
    for (const detachOne of detach) {
      detachOne();
    }
  };
}

function attachTransportListener(
  transport: SipVoiceBridgeTransport,
  type: SipVoiceBridgeTransportEventType,
  listener: SipVoiceBridgeTransportListener,
): () => void {
  if (transport.addEventListener) {
    transport.addEventListener(type, listener);
    return () => transport.removeEventListener?.(type, listener);
  }

  const property = `on${type}` as const;
  const previous = transport[property] ?? null;
  const wrapped: SipVoiceBridgeTransportListener = (event) => {
    previous?.(event);
    listener(event);
  };
  transport[property] = wrapped;
  return () => {
    if (transport[property] === wrapped) {
      transport[property] = previous;
    }
  };
}

function readTransportMessageData(event: unknown): unknown {
  if (event && typeof event === "object" && "data" in event) {
    return (event as { data: unknown }).data;
  }
  return event;
}

function bridgeErrorEventToError(event: ErrorEvent): Error {
  const error = new Error(event.error.message);
  error.name = `SipVoiceBridgeError:${event.error.code}`;
  return error;
}

function toError(error: unknown, fallbackMessage: string): Error {
  if (error instanceof Error) {
    return error;
  }
  if (error && typeof error === "object" && "message" in error) {
    const message = (error as { message: unknown }).message;
    if (typeof message === "string" && message) {
      return new Error(message);
    }
  }
  return new Error(fallbackMessage);
}

export type { AudioFrame };
