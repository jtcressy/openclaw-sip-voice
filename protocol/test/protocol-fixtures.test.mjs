import assert from "node:assert/strict";
import test from "node:test";

import {
  PROTOCOL_VERSION,
  SUPPORTED_PROTOCOL_VERSIONS,
  UNSUPPORTED_VERSION_CLOSE_CODE,
  runFixtureValidation,
  validateMessage
} from "../validator.mjs";

test("protocol fixtures match the local bridge contract", async () => {
  const result = await runFixtureValidation();
  assert.deepEqual(result.failures, []);
  assert.equal(result.ok, true);
});

test("unsupported protocol versions fail deterministically", () => {
  const result = validateMessage({
    protocolVersion: "9.9",
    type: "hello",
    sentAt: "2026-05-05T17:00:00.000Z"
  });

  assert.equal(result.ok, false);
  assert.equal(result.code, "unsupported_protocol_version");
  assert.equal(result.fatal, true);
  assert.equal(result.closeCode, UNSUPPORTED_VERSION_CLOSE_CODE);
  assert.deepEqual(result.expectedProtocolVersions, SUPPORTED_PROTOCOL_VERSIONS);
  assert.equal(result.event.protocolVersion, PROTOCOL_VERSION);
  assert.equal(result.event.type, "error");
  assert.equal(result.event.fatal, true);
  assert.equal(result.event.error.code, "unsupported_protocol_version");
  assert.equal(result.event.error.retryable, false);
  assert.deepEqual(result.event.error.expectedProtocolVersions, SUPPORTED_PROTOCOL_VERSIONS);
});
