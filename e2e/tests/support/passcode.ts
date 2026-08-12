import { execFileSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import path from "node:path";

/**
 * Reading a one-time passcode out of the dev stack's API log.
 *
 * Shared by every spec whose journey needs a real sign-in — the Follow intent
 * round trip and the sign-in consent step — because a passcode exists nowhere
 * but in the log of the API that issued it, and two copies of that reasoning
 * would be two things to fix when the log line changes.
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
 */
function readApiLog(): string | null {
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
 * The passcode for one address, read out of the API's own log.
 *
 * `make dev` runs on the logging email sender (no RESEND_API_KEY), which writes
 * one line per passcode: {"msg":"otp sent","email":…,"code":…}. That log is the
 * only place a code exists in a dev stack — the database stores a salted hash —
 * and it is why the checkout spec avoids signing in at all.
 *
 * Returns null when the stack cannot be reached that way, which is a real case:
 * PLAYWRIGHT_BASE_URL exists so this suite can be pointed at a build that is not
 * the compose stack, and these journeys cannot run against one.
 */
export function readPasscode(email: string): string | null {
  const logs = readApiLog();
  if (logs === null) return null;

  // Last match wins: a re-run issues a second passcode for a new address, and
  // only the most recent line for THIS address is live.
  let code: string | null = null;
  for (const line of logs.split("\n")) {
    if (!line.includes('"otp sent"') || !line.includes(`"${email}"`)) continue;
    const match = /"code":"(\d{6})"/.exec(line);
    if (match) code = match[1];
  }
  return code;
}
