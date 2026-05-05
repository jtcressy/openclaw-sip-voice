import path from "node:path";
import { registerHooks } from "node:module";
import { fileURLToPath, pathToFileURL } from "node:url";

const pluginRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const realtimeFakeUrl = pathToFileURL(
  path.join(pluginRoot, "test/fakes/openclaw-realtime-voice.mjs"),
).href;

registerHooks({
  resolve(specifier, context, nextResolve) {
    if (specifier === "openclaw/plugin-sdk/realtime-voice") {
      return {
        shortCircuit: true,
        url: realtimeFakeUrl,
      };
    }
    if (specifier.startsWith(".") && specifier.endsWith(".js")) {
      return nextResolve(`${specifier.slice(0, -3)}.ts`, context);
    }
    return nextResolve(specifier, context);
  },
});
