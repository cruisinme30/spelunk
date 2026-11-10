// MCP setup must generate a working command and argv when the client runs
// elsewhere and both the installed command and repository contain spaces.
// @covers screen:agent-access command:installCommand
import assert from "node:assert/strict";
import { execFileSync, spawn, spawnSync } from "node:child_process";
import { once } from "node:events";
import { mkdirSync, mkdtempSync, renameSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { delimiter, join } from "node:path";
import { createInterface } from "node:readline";
import { test } from "node:test";
import { installCommand } from "../commandInstall";
import { DAEMON_BINARY, SKIP_WITHOUT_DAEMON as skip } from "./realDaemon";

interface MCPConfig {
  mcpServers: { spelunk: { command: string; args: string[] } };
}

/** Read and check one JSON-RPC reply from the actual executable's stdout. */
async function reply(lines: AsyncIterator<string>, id: number): Promise<Record<string, unknown>> {
  const line = await lines.next();
  assert.equal(line.done, false, "server closed stdout before replying");
  const response = JSON.parse(line.value) as {
    jsonrpc: string;
    id: number;
    error?: unknown;
    result: Record<string, unknown>;
  };
  assert.equal(response.jsonrpc, "2.0");
  assert.equal(response.id, id);
  assert.equal(response.error, undefined);
  return response.result;
}

test(
  "generated MCP config launches the installed command from an unrelated cwd",
  { skip, timeout: 10_000 },
  async (t) => {
    const folder = mkdtempSync(join(tmpdir(), "spelunk mcp "));
    t.after(() => {
      rmSync(folder, { recursive: true, force: true });
    });
    const bin = join(folder, "bin with spaces");
    const installed = join(bin, process.platform === "win32" ? "spelunk.exe" : "spelunk");
    installCommand(DAEMON_BINARY, { file: installed, onPath: true });
    const repo = join(folder, "configured repo");
    const cwd = join(folder, "unrelated cwd");
    mkdirSync(repo);
    mkdirSync(cwd);
    const env = { ...process.env, PATH: `${bin}${delimiter}${process.env["PATH"] ?? ""}` };
    const configArguments = ["mcp", "config", "--in", repo];
    const json = execFileSync(installed, configArguments, { cwd, env, encoding: "utf8", timeout: 5000 });
    assert.equal(json, execFileSync(installed, configArguments, { cwd, env, encoding: "utf8", timeout: 5000 }));
    const entry = (JSON.parse(json) as MCPConfig).mcpServers.spelunk;
    assert.equal(entry.command, installed);
    assert.deepEqual(entry.args, ["mcp", "--in", repo]);

    const child = spawn(entry.command, entry.args, { cwd, env, stdio: "pipe" });
    const exited = once(child, "close");
    let stderr = "";
    child.stderr.on("data", (chunk: Buffer) => {
      stderr += chunk.toString();
    });
    const output = createInterface({ input: child.stdout });
    const lines = output[Symbol.asyncIterator]();
    t.after(() => {
      output.close();
      child.kill();
    });
    child.stdin.write(
      JSON.stringify({
        jsonrpc: "2.0",
        id: 1,
        method: "initialize",
        params: { protocolVersion: "2025-11-25", capabilities: {}, clientInfo: { name: "setup-test", version: "1" } },
      }) + "\n",
    );
    assert.equal((await reply(lines, 1))["protocolVersion"], "2025-11-25");
    child.stdin.write('{"jsonrpc":"2.0","method":"notifications/initialized"}\n');
    child.stdin.write('{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}\n');
    const tools = (await reply(lines, 2))["tools"];
    assert.ok(Array.isArray(tools));
    assert.equal(tools.length, 4);
    child.stdin.write(
      '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"spelunk_syntax","arguments":{}}}\n',
    );
    assert.match(JSON.stringify(await reply(lines, 3)), /Spelunk query language/);
    child.stdin.end();
    assert.deepEqual(await exited, [0, null], stderr);

    // The explicit folder must be used at launch, not silently replaced by
    // the client's cwd. This check needs no socket or persisted index.
    renameSync(repo, join(folder, "moved repo"));
    const missing = spawnSync(entry.command, entry.args, { cwd, env, input: "", encoding: "utf8", timeout: 5000 });
    assert.equal(missing.status, 2);
    assert.ok(missing.stderr.includes(repo), missing.stderr);
  },
);
