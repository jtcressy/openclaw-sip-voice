import { runFixtureValidation } from "./validator.mjs";

const result = await runFixtureValidation();

if (result.ok) {
  console.log(`Protocol validation passed (${result.checked} fixtures checked).`);
  process.exit(0);
}

console.error("Protocol validation failed.");
for (const failure of result.failures) {
  console.error(`- ${failure.file}`);
  for (const error of failure.errors) {
    console.error(`  - ${error}`);
  }
}

process.exit(1);
