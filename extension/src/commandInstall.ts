// The spelunk command outside VS Code: where Install 'spelunk' Command in
// PATH puts it, the settings it reads, and the note Add to Claude Code
// offers for CLAUDE.md. No vscode import, so it runs in plain unit tests.
import {
  chmodSync,
  copyFileSync,
  existsSync,
  mkdirSync,
  readFileSync,
  renameSync,
  statSync,
  writeFileSync,
} from "node:fs";
import { dirname, join, posix, win32 } from "node:path";
import type { Settings } from "./protocol.gen";

/** Where the command goes, and whether that folder is on the PATH. */
export interface InstallTarget {
  file: string;
  onPath: boolean;
}

/**
 * Where Install puts the command: ~/.local/bin/spelunk on macOS and Linux, where
 * Python's command installers put theirs, and %LOCALAPPDATA%\Programs\Spelunk\spelunk.exe on Windows.
 */
export function installTarget(platform: NodeJS.Platform, home: string, env: NodeJS.ProcessEnv): InstallTarget {
  const windows = platform === "win32";
  const path = windows ? win32 : posix;
  const folder = windows
    ? path.join(env["LOCALAPPDATA"] ?? path.join(home, "AppData", "Local"), "Programs", "Spelunk")
    : path.join(home, ".local", "bin");
  const onPath = (env["PATH"] ?? env["Path"] ?? "")
    .split(path.delimiter)
    .some((entry) => samePath(entry, folder, windows));
  return { file: path.join(folder, windows ? "spelunk.exe" : "spelunk"), onPath };
}

/** Whether two folder paths name the same folder, ignoring a trailing separator (and case on Windows). */
function samePath(a: string, b: string, windows: boolean): boolean {
  return windows
    ? withoutTrailingSeparator(a).toLowerCase() === withoutTrailingSeparator(b).toLowerCase()
    : withoutTrailingSeparator(a) === withoutTrailingSeparator(b);
}

/** A path without the separators it ends with. */
function withoutTrailingSeparator(path: string): string {
  return path.replace(/[\\/]+$/, "");
}

/**
 * Copies the daemon binary to the target, replacing an older copy in one
 * step. A copy rather than a link: the extension's folder changes with
 * every update, and refreshInstalled keeps the copy current.
 */
export function installCommand(binary: string, target: InstallTarget): void {
  mkdirSync(dirname(target.file), { recursive: true });
  const temporary = `${target.file}.new`;
  copyFileSync(binary, temporary);
  chmodSync(temporary, 0o755);
  renameSync(temporary, target.file);
}

/**
 * Brings an installed copy up to date with the bundled binary after an
 * extension update. Nothing happens when the command was never installed.
 */
export function refreshInstalled(binary: string, target: InstallTarget): boolean {
  if (!existsSync(target.file) || !existsSync(binary)) return false;
  const installed = statSync(target.file);
  const bundled = statSync(binary);
  if (installed.size === bundled.size && installed.mtimeMs >= bundled.mtimeMs) return false;
  installCommand(binary, target);
  return true;
}

/** The arguments of Claude Code's own command that adds Spelunk to every project. */
export function claudeAddArguments(command: string): string[] {
  return ["mcp", "add", "--scope", "user", "spelunk", "--", command, "mcp"];
}

/** The heading Add to Claude Code's note starts with, so it is added once. */
const NOTE_HEADING = "## Searching this repo";

/** The note for CLAUDE.md: when Claude should reach for Spelunk. Agents use grep unless told. */
export const CLAUDE_NOTE = `${NOTE_HEADING}

Use the spelunk MCP tools (spelunk_search, spelunk_syntax) instead of grep, find or git log to find definitions,
references, commits and the lines they changed, or anything in a large repo: they search Spelunk's index.
`;

/** CLAUDE.md's text with the note added at the end, or undefined when it is already there. */
export function withClaudeNote(existing?: string): string | undefined {
  if (existing?.includes(NOTE_HEADING)) return undefined;
  if (!existing?.trim()) return CLAUDE_NOTE;
  return `${existing.replace(/\s*$/, "")}\n\n${CLAUDE_NOTE}`;
}

/** Adds the note to the CLAUDE.md in `folder`; false when it was already there. */
export function addClaudeNote(folder: string): boolean {
  const path = join(folder, "CLAUDE.md");
  const updated = withClaudeNote(existsSync(path) ? readFileSync(path, "utf8") : undefined);
  if (updated === undefined) return false;
  writeFileSync(path, updated);
  return true;
}

/** Where the extension records the settings the command's background reader uses. */
export function commandConfigPath(home: string): string {
  return join(home, ".spelunk", "cli.json");
}

/**
 * Records the window's index settings for the command: its reader then
 * finds the indexes where this window keeps them, and re-reads changed
 * files under the same rules. Unchanged settings aren't written again.
 */
export function writeCommandConfig(path: string, settings: Settings): boolean {
  const text = `${JSON.stringify({ settings }, null, 2)}\n`;
  if (existsSync(path) && readFileSync(path, "utf8") === text) return false;
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, text, { mode: 0o600 });
  return true;
}
