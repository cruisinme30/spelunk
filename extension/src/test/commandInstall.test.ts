// The spelunk command outside VS Code: where it installs, how it stays
// current, the settings file it reads, and the CLAUDE.md note.
// @covers command:installCommand command:addToClaudeCode
import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, readFileSync, statSync, utimesSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import {
  CLAUDE_NOTE,
  claudeAddArguments,
  installCommand,
  installTarget,
  refreshInstalled,
  withClaudeNote,
  writeCommandConfig,
} from "../commandInstall";
import { daemonSettings, type ConfigReader } from "../settings";

const defaultsOnly: ConfigReader = { get: (_key, fallback) => fallback };

test("the command installs beside other user commands, and says whether that is on the PATH", () => {
  assert.deepEqual(installTarget("darwin", "/Users/me", { PATH: "/usr/bin:/Users/me/.local/bin/" }), {
    file: "/Users/me/.local/bin/spelunk",
    onPath: true,
  });
  assert.equal(installTarget("linux", "/home/me", { PATH: "/usr/bin" }).onPath, false);
  const windows = installTarget("win32", String.raw`C:\Users\me`, {
    LOCALAPPDATA: String.raw`C:\Users\me\AppData\Local`,
    Path: String.raw`C:\Windows;c:\users\me\appdata\local\programs\spelunk`,
  });
  assert.ok(windows.file.endsWith("spelunk.exe"));
  assert.equal(windows.onPath, true);
});

test("installing copies the binary, executable, and an extension update refreshes the copy", () => {
  const folder = mkdtempSync(join(tmpdir(), "spelunk-install-"));
  const binary = join(folder, "spelunk-daemon");
  writeFileSync(binary, "v1");
  const target = { file: join(folder, "bin", "spelunk"), onPath: false };
  assert.equal(refreshInstalled(binary, target), false, "nothing to refresh before an install");
  installCommand(binary, target);
  assert.equal(readFileSync(target.file, "utf8"), "v1");
  assert.equal(statSync(target.file).mode & 0o111, 0o111);
  assert.equal(refreshInstalled(binary, target), false, "an up-to-date copy is left alone");
  writeFileSync(binary, "version 2");
  const later = new Date(Date.now() + 5000);
  utimesSync(binary, later, later);
  assert.equal(refreshInstalled(binary, target), true);
  assert.equal(readFileSync(target.file, "utf8"), "version 2");
});

test("Add to Claude Code runs Claude Code's own command for every project", () => {
  assert.deepEqual(claudeAddArguments("/Users/me/.local/bin/spelunk"), [
    "mcp",
    "add",
    "--scope",
    "user",
    "spelunk",
    "--",
    "/Users/me/.local/bin/spelunk",
    "mcp",
  ]);
});

test("the CLAUDE.md note is added once, after what is there", () => {
  assert.equal(withClaudeNote(), CLAUDE_NOTE);
  assert.equal(withClaudeNote("# My repo\n\nBuild with make.\n\n"), `# My repo\n\nBuild with make.\n\n${CLAUDE_NOTE}`);
  assert.equal(withClaudeNote(`# My repo\n\n${CLAUDE_NOTE}`), undefined);
});

test("the command's settings file records this window's index settings, and isn't rewritten unchanged", () => {
  const folder = mkdtempSync(join(tmpdir(), "spelunk-config-"));
  mkdirSync(folder, { recursive: true });
  const path = join(folder, ".spelunk", "cli.json");
  const settings = { ...daemonSettings(defaultsOnly, "/home/me"), location: "/data/spelunk" };
  assert.equal(writeCommandConfig(path, settings), true);
  assert.equal(
    (JSON.parse(readFileSync(path, "utf8")) as { settings: { location: string } }).settings.location,
    "/data/spelunk",
  );
  assert.equal(statSync(path).mode & 0o777, 0o600);
  assert.equal(writeCommandConfig(path, settings), false);
});
