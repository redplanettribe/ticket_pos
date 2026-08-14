import { execFileSync } from "node:child_process";
import { existsSync } from "node:fs";
import path from "node:path";

/**
 * Reading a one-time passcode out of the PARITY stack's API log.
 *
 * A sibling of tests/support/passcode.ts rather than a reuse of it, because the
 * two read different stacks: that one names docker-compose.yml and the service
 * `backend`, this one names docker-compose.prod.yml and the service `api`. The
 * shared part is four lines of regex over a log line; the part that differs is
 * the part that would silently read the wrong containers.
 *
 * The parity API runs without RESEND_API_KEY, so it uses the logging email
 * sender and writes one line per passcode:
 * `{"msg":"otp sent","email":…,"code":…}`. That log is the only place a code
 * exists — the database stores a salted hash.
 */

/**
 * The parity compose file, found by walking up from wherever the suite was
 * invoked. Naming the file rather than relying on the ambient compose project
 * matters: `docker compose` keys a project on its directory, so a run from a git
 * worktree would otherwise address a project that does not exist and read no
 * logs at all.
 */
function findComposeFile(): string | null {
  let directory = process.cwd();
  for (let depth = 0; depth < 6; depth += 1) {
    const candidate = path.join(directory, "docker-compose.prod.yml");
    if (existsSync(candidate)) return candidate;
    const parent = path.dirname(directory);
    if (parent === directory) break;
    directory = parent;
  }
  return null;
}

/**
 * The passcode for one address, or null when this run cannot reach the parity
 * stack's logs that way.
 *
 * Null is a real and expected answer — STAFF_URL exists so the suite can be
 * pointed at an image that is not the compose stack — and the caller skips
 * rather than fails, because a journey that needs a passcode cannot run without
 * one and a red test would be reporting the environment, not the code.
 */
export function readParityPasscode(email: string): string | null {
  const composeFile = findComposeFile();
  if (!composeFile) return null;

  let logs: string;
  try {
    logs = execFileSync(
      "docker",
      ["compose", "-f", composeFile, "logs", "--no-log-prefix", "--since", "5m", "api"],
      { encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] },
    );
  } catch {
    return null;
  }

  // Last match wins: only the most recent line for THIS address is live.
  let code: string | null = null;
  for (const line of logs.split("\n")) {
    if (!line.includes('"otp sent"') || !line.includes(`"${email}"`)) continue;
    const match = /"code":"(\d{6})"/.exec(line);
    if (match) code = match[1];
  }
  return code;
}
