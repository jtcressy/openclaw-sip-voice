import { readdir, readFile } from "node:fs/promises";
import path from "node:path";

const forbidden = [
  { label: "node:net import", pattern: /["']node:net["']/ },
  { label: "node:tls import", pattern: /["']node:tls["']/ },
  { label: "node:dgram import", pattern: /["']node:dgram["']/ },
  { label: "net.connect(", pattern: /\bnet\s*\.\s*connect\s*\(/ },
  { label: "tls.connect(", pattern: /\btls\s*\.\s*connect\s*\(/ },
  { label: "dgram.createSocket(", pattern: /\bdgram\s*\.\s*createSocket\s*\(/ },
  { label: "new WebSocket", pattern: /\bnew\s+WebSocket\b/ },
  { label: ".listen(", pattern: /\.listen\s*\(/ },
  { label: "createServer(", pattern: /\bcreateServer\s*\(/ },
];

const roots = [process.cwd()];
const ignoredDirectories = new Set(["node_modules", "dist", "build", "coverage"]);
const failures = [];

while (roots.length > 0) {
  const current = roots.pop();
  const entries = await readdir(current, { withFileTypes: true });

  for (const entry of entries) {
    const filePath = path.join(current, entry.name);
    if (entry.isDirectory()) {
      if (!ignoredDirectories.has(entry.name)) {
        roots.push(filePath);
      }
      continue;
    }

    if (!entry.isFile() || !entry.name.endsWith(".ts")) {
      continue;
    }

    const content = await readFile(filePath, "utf8");
    for (const rule of forbidden) {
      if (rule.pattern.test(content)) {
        failures.push(`${path.relative(process.cwd(), filePath)} matches ${rule.label}`);
      }
    }
  }
}

if (failures.length > 0) {
  console.error("plugin foundation must not open sockets or servers during registration");
  for (const failure of failures) {
    console.error(`- ${failure}`);
  }
  process.exitCode = 1;
}
