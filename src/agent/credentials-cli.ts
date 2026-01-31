#!/usr/bin/env node
/**
 * Credentials CLI
 *
 * Commands:
 *   init       Create credentials.json5 and skills.env.json5
 */

import { existsSync, mkdirSync, writeFileSync, chmodSync } from "node:fs";
import { dirname } from "node:path";
import { getCredentialsPath, getSkillsEnvPath } from "./credentials.js";

type Command = "init" | "help";

function printUsage(): void {
  console.log("Usage: pnpm credentials:cli <command> [options]");
  console.log("");
  console.log("Commands:");
  console.log("  init        Create credentials.json5 and skills.env.json5 (empty templates)");
  console.log("  help        Show this help");
  console.log("");
  console.log("Options:");
  console.log("  --force         Overwrite existing files");
  console.log("  --core-only     Only create credentials.json5");
  console.log("  --skills-only   Only create skills.env.json5");
  console.log("  --path          Override credentials path (SMC_CREDENTIALS_PATH)");
  console.log("  --skills-path   Override skills env path (SMC_SKILLS_ENV_PATH)");
  console.log("");
  console.log("Examples:");
  console.log("  pnpm credentials:cli init");
  console.log("  pnpm credentials:cli init --force");
  console.log("  pnpm credentials:cli init --core-only");
  console.log("  pnpm credentials:cli init --skills-only");
}

function buildCoreTemplate(): string {
  return `{
  version: 1,
  llm: {
    // provider: "openai",
    providers: {
      // openai: { apiKey: "sk-...", baseUrl: "https://api.openai.com/v1", model: "gpt-4.1" }
    }
  },
  tools: {
    // brave: { apiKey: "brv-..." },
    // perplexity: { apiKey: "pplx-...", baseUrl: "https://api.perplexity.ai", model: "perplexity/sonar-pro" }
  }
}
`;
}

function buildSkillsTemplate(): string {
  return `{
  env: {
    // Dynamic keys (skills, plugins, integrations)
    // LINEAR_API_KEY: "lin-..."
  }
}
`;
}

function parseArgs(argv: string[]) {
  const args = [...argv];
  let force = false;
  let pathOverride: string | undefined;
  let skillsPathOverride: string | undefined;
  let coreOnly = false;
  let skillsOnly = false;
  const positional: string[] = [];

  while (args.length > 0) {
    const arg = args.shift();
    if (!arg) break;
    if (arg === "--force" || arg === "-f") {
      force = true;
      continue;
    }
    if (arg === "--core-only") {
      coreOnly = true;
      continue;
    }
    if (arg === "--skills-only") {
      skillsOnly = true;
      continue;
    }
    if (arg === "--path") {
      pathOverride = args.shift();
      continue;
    }
    if (arg === "--skills-path") {
      skillsPathOverride = args.shift();
      continue;
    }
    if (arg === "--help" || arg === "-h") {
      return { command: "help" as Command, force, pathOverride, skillsPathOverride, coreOnly, skillsOnly };
    }
    positional.push(arg);
  }

  const command = (positional[0] || "help") as Command;
  return { command, force, pathOverride, skillsPathOverride, coreOnly, skillsOnly };
}

function cmdInit(force: boolean, pathOverride?: string, skillsPathOverride?: string, coreOnly?: boolean, skillsOnly?: boolean): void {
  const createCore = skillsOnly ? false : true;
  const createSkills = coreOnly ? false : true;

  if (!createCore && !createSkills) {
    console.error("Error: both --core-only and --skills-only were provided.");
    process.exit(1);
  }

  if (createCore) {
    const path = pathOverride ?? getCredentialsPath();
    if (existsSync(path) && !force) {
      console.error(`Error: credentials file already exists at ${path}`);
      console.error("Use --force to overwrite.");
      process.exit(1);
    }
    mkdirSync(dirname(path), { recursive: true });
    writeFileSync(path, buildCoreTemplate(), "utf8");
    chmodSync(path, 0o600);
    console.log(`Created: ${path}`);
  }

  if (createSkills) {
    const skillsPath = skillsPathOverride ?? getSkillsEnvPath();
    if (existsSync(skillsPath) && !force) {
      console.error(`Error: skills env file already exists at ${skillsPath}`);
      console.error("Use --force to overwrite.");
      process.exit(1);
    }
    mkdirSync(dirname(skillsPath), { recursive: true });
    writeFileSync(skillsPath, buildSkillsTemplate(), "utf8");
    chmodSync(skillsPath, 0o600);
    console.log(`Created: ${skillsPath}`);
  }

  console.log("Edit these files to add your credentials.");
}

async function main() {
  const { command, force, pathOverride, skillsPathOverride, coreOnly, skillsOnly } = parseArgs(process.argv.slice(2));

  switch (command) {
    case "init":
      cmdInit(force, pathOverride, skillsPathOverride, coreOnly, skillsOnly);
      break;
    case "help":
    default:
      printUsage();
      break;
  }
}

main().catch((err) => {
  console.error(err?.stack || String(err));
  process.exit(1);
});
