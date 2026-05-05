export const REALTIME_VOICE_AUDIO_FORMAT_G711_ULAW_8KHZ = {
  encoding: "g711_ulaw",
  sampleRateHz: 8000,
  channels: 1,
};

export function resolveConfiguredRealtimeVoiceProvider(params) {
  const provider = params.providers?.find(
    (candidate) => candidate.id === params.configuredProviderId,
  );
  if (!provider) {
    throw new Error(params.noRegisteredProviderMessage ?? "No realtime provider registered");
  }

  const rawConfig = {
    ...(params.providerConfigs?.[params.configuredProviderId] ?? {}),
    model: params.defaultModel,
  };
  const providerConfig = provider.resolveConfig
    ? provider.resolveConfig({ rawConfig, cfg: params.cfg })
    : rawConfig;

  if (provider.isConfigured) {
    const configured = provider.isConfigured({
      providerConfig,
      cfg: params.cfg,
    });
    if (!configured) {
      throw new Error(`Realtime provider ${provider.id} is not configured`);
    }
  }

  return {
    provider,
    providerConfig,
  };
}

export function createRealtimeVoiceBridgeSession(params) {
  let bridge;
  const request = {
    providerConfig: params.providerConfig,
    audioFormat: params.audioFormat,
    instructions: params.instructions,
    onAudio: (audio) => params.audioSink.sendAudio(audio),
    onClearAudio: () => params.audioSink.clearAudio?.(),
    onMark: (markName) => {
      if (params.markStrategy === "ack-immediately") {
        bridge?.acknowledgeMark?.(markName);
        return;
      }
      params.audioSink.sendMark?.(markName);
    },
    onReady: () => {
      if (params.triggerGreetingOnReady) {
        bridge?.triggerGreeting?.(params.initialGreetingInstructions);
      }
    },
  };
  bridge = params.provider.createBridge(request);
  return {
    bridge,
    close: () => bridge?.close?.(),
  };
}
