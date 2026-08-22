import { execFileSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import path from "node:path";

/**
 * Reading the dev stack's API log.
 *
 * `make dev` runs on the logging email sender (no RESEND_API_KEY), so every
 * message the platform "sends" is one JSON line in the API's log and nowhere
 * else. Any journey that needs what a mail carried — a passcode, an Assignment
 * Link — reads it from here, through one reader, so the log line changing is
 * one thing to fix.
 */

/**
 * The dev stack's compose file, found by walking up from wherever this suite was
 * invoked — `pnpm test` from e2e/, `pnpm --filter` from the root, either works.
 *
 * Naming the file rather than relying on the ambient compose project matters:
 * `docker compose` keys a project on its directory, so a suite run from a git
 * worktree would otherwise address a project that does not exist and read no
 * logs at all.
 */
function findComposeFile(): string | null {
  let directory = process.cwd();
  for (let depth = 0; depth < 6; depth += 1) {
    const candidate = path.join(directory, "docker-compose.yml");
    if (existsSync(candidate)) return candidate;
    const parent = path.dirname(directory);
    if (parent === directory) break;
    directory = parent;
  }
  return null;
}

/**
 * The API's recent log, from wherever this run's API is writing it.
 *
 * The compose stack is the ordinary case. `E2E_API_LOG_FILE` is the companion to
 * PLAYWRIGHT_BASE_URL and exists for the same case the README already describes:
 * a branch build running on its own ports, whose API is a plain process rather
 * than a container. Without it that configuration could not run these specs at
 * all, since a passcode exists nowhere but in the log of the API that issued it.
 *
 * Returns null when the stack cannot be reached that way, which is a real case:
 * PLAYWRIGHT_BASE_URL exists so this suite can be pointed at a build that is not
 * the compose stack, and these journeys cannot run against one.
 */
export function readApiLog(): string | null {
  const file = process.env.E2E_API_LOG_FILE?.trim();
  if (file) {
    try {
      return readFileSync(file, "utf8");
    } catch {
      return null;
    }
  }

  const composeFile = findComposeFile();
  if (!composeFile) return null;
  try {
    return execFileSync(
      "docker",
      ["compose", "-f", composeFile, "logs", "--no-log-prefix", "--since", "5m", "backend"],
      { encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] },
    );
  } catch {
    return null;
  }
}

/**
 * The last log line carrying `msg` that mentions `email`, or null when there is
 * none — or no log at all.
 *
 * Last match wins: a re-run mails a second time for a new address, and only the
 * most recent line for THIS address is live.
 */
export function lastMailLine(msg: string, email: string): string | null {
  const logs = readApiLog();
  if (logs === null) return null;

  let found: string | null = null;
  for (const line of logs.split("\n")) {
    if (line.includes(`"${msg}"`) && line.includes(`"${email}"`)) found = line;
  }
  return found;
}
