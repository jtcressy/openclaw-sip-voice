import {
  createRealtimeVoiceBridgeSession,
  REALTIME_VOICE_AUDIO_FORMAT_G711_ULAW_8KHZ,
  resolveConfiguredRealtimeVoiceProvider,
  type RealtimeVoiceAudioSink,
  type RealtimeVoiceBridgeSession,
  type RealtimeVoiceBridgeSessionParams,
  type RealtimeVoiceProviderPlugin,
} from "openclaw/plugin-sdk/realtime-voice";
import type { OpenClawConfig } from "openclaw/plugin-sdk/plugin-entry";
import type { SipVoiceConfig } from "./config.js";

export function resolveSipVoiceRealtimeAudioFormat(config: SipVoiceConfig) {
  if (config.audio.format !== "g711-ulaw-8khz") {
    throw new Error(`Unsupported SIP voice audio format: ${config.audio.format}`);
  }
  return REALTIME_VOICE_AUDIO_FORMAT_G711_ULAW_8KHZ;
}

export function resolveSipVoiceRealtimeProvider(params: {
  config: SipVoiceConfig;
  fullConfig: OpenClawConfig;
  providers?: RealtimeVoiceProviderPlugin[];
}) {
  return resolveConfiguredRealtimeVoiceProvider({
    configuredProviderId: params.config.realtime.provider,
    providerConfigs: params.config.realtime.providers,
    cfg: params.fullConfig,
    providers: params.providers,
    defaultModel: params.config.realtime.model,
    noRegisteredProviderMessage: "No configured realtime voice provider registered for sip-voice",
  });
}

export function createSipVoiceRealtimeBridgeSession(params: {
  config: SipVoiceConfig;
  fullConfig: OpenClawConfig;
  audioSink: RealtimeVoiceAudioSink;
  providers?: RealtimeVoiceProviderPlugin[];
  session?: Partial<
    Omit<
      RealtimeVoiceBridgeSessionParams,
      | "provider"
      | "providerConfig"
      | "audioFormat"
      | "audioSink"
      | "instructions"
      | "initialGreetingInstructions"
      | "markStrategy"
    >
  >;
}): RealtimeVoiceBridgeSession {
  const resolved = resolveSipVoiceRealtimeProvider({
    config: params.config,
    fullConfig: params.fullConfig,
    providers: params.providers,
  });

  return createRealtimeVoiceBridgeSession({
    ...params.session,
    provider: resolved.provider,
    providerConfig: resolved.providerConfig,
    audioFormat: resolveSipVoiceRealtimeAudioFormat(params.config),
    audioSink: params.audioSink,
    instructions: params.config.realtime.instructions,
    initialGreetingInstructions: params.config.realtime.introMessage,
    markStrategy: "ack-immediately",
    triggerGreetingOnReady: params.session?.triggerGreetingOnReady ?? false,
  });
}
