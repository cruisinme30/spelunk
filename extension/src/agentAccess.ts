// Spelunk for agents and terminals, from VS Code's side: the commands that
// install the spelunk command and add it to Claude Code, the MCP server
// VS Code's own agents (Copilot) find, and the settings file the command
// reads. The command and the MCP server are the daemon binary itself.
import { execFile } from "node:child_process";
import { homedir } from "node:os";
import { dirname } from "node:path";
import * as vscode from "vscode";
import {
  addClaudeNote,
  claudeAddArguments,
  commandConfigPath,
  installCommand,
  installTarget,
  refreshInstalled,
  writeCommandConfig,
} from "./commandInstall";
import type { Settings } from "./protocol.gen";

/** What the agent features need from the extension. */
export interface AgentAccessOptions {
  binary: string;
  version: string;
  settings: () => Settings;
  offerToCopilot: () => boolean;
  log: (line: string) => void;
}

/** Registers the commands and the MCP server, and keeps the command's settings file current. */
export function registerAgentAccess(options: AgentAccessOptions): vscode.Disposable[] {
  const target = installTarget(process.platform, homedir(), process.env);
  try {
    if (refreshInstalled(options.binary, target)) options.log(`[agents] updated ${target.file}`);
  } catch (error) {
    options.log(`[agents] couldn't update ${target.file}: ${String(error)}`);
  }
  const recordSettings = () => {
    try {
      writeCommandConfig(commandConfigPath(homedir()), options.settings());
    } catch (error) {
      options.log(`[agents] couldn't record settings for the spelunk command: ${String(error)}`);
    }
  };
  recordSettings();
  const changed = new vscode.EventEmitter<void>();
  return [
    changed,
    vscode.commands.registerCommand("spelunk.installCommand", () => install(options.binary)),
    vscode.commands.registerCommand("spelunk.addToClaudeCode", () => addToClaudeCode(options.binary)),
    vscode.workspace.onDidChangeConfiguration((event) => {
      if (event.affectsConfiguration("spelunk") || event.affectsConfiguration("files.exclude")) recordSettings();
      if (event.affectsConfiguration("spelunk.agent.offerToCopilot")) changed.fire();
    }),
    vscode.workspace.onDidChangeWorkspaceFolders(() => {
      changed.fire();
    }),
    ...registerMcpServer(options, changed.event),
  ];
}

/**
 * Lists Spelunk for VS Code's agents. The API arrived in VS Code 1.101;
 * older versions don't have it, and Spelunk works there without it.
 */
function registerMcpServer(options: AgentAccessOptions, changed: vscode.Event<void>): vscode.Disposable[] {
  const lm = vscode.lm as Partial<typeof vscode.lm> | undefined;
  if (
    typeof lm?.registerMcpServerDefinitionProvider !== "function" ||
    typeof vscode.McpStdioServerDefinition !== "function"
  ) {
    options.log("[agents] this VS Code can't list MCP servers; Copilot won't see Spelunk");
    return [];
  }
  return [
    lm.registerMcpServerDefinitionProvider("spelunk", {
      onDidChangeMcpServerDefinitions: changed,
      provideMcpServerDefinitions: () => {
        if (!options.offerToCopilot()) return [];
        const server = new vscode.McpStdioServerDefinition("Spelunk", options.binary, ["mcp"], {}, options.version);
        const folder = vscode.workspace.workspaceFolders?.find((each) => each.uri.scheme === "file");
        if (folder) server.cwd = folder.uri;
        return [server];
      },
    }),
  ];
}

/** Install 'spelunk' Command in PATH. */
function install(binary: string): string | undefined {
  const target = installTarget(process.platform, homedir(), process.env);
  try {
    installCommand(binary, target);
  } catch (error) {
    void vscode.window.showErrorMessage(`Couldn't install the spelunk command at ${target.file}: ${String(error)}`);
    return undefined;
  }
  const folder = dirname(target.file);
  const next = target.onPath
    ? "Try spelunk search 'symbol:main' in a new terminal."
    : `Add ${folder} to your PATH, then try spelunk search 'symbol:main' in a new terminal.`;
  void vscode.window.showInformationMessage(`Installed spelunk at ${target.file}. ${next}`);
  return target.file;
}

/** Add to Claude Code: asks, runs Claude Code's own claude mcp add, then offers the CLAUDE.md note. */
async function addToClaudeCode(binary: string): Promise<void> {
  const target = installTarget(process.platform, homedir(), process.env);
  const args = claudeAddArguments(target.file);
  const add = "Add";
  const answer = await vscode.window.showInformationMessage(
    "Add Spelunk to Claude Code for every project? Claude Code can then search your code and history with Spelunk.",
    { modal: true, detail: `This installs the spelunk command and runs:\nclaude ${args.join(" ")}` },
    add,
  );
  if (answer !== add) return;
  if (install(binary) === undefined) return;
  try {
    await run("claude", args);
  } catch (error) {
    const missing = (error as NodeJS.ErrnoException).code === "ENOENT";
    void vscode.window.showErrorMessage(
      missing
        ? "Claude Code's claude command isn't on the PATH. Install Claude Code, then run Spelunk: Add to Claude Code again."
        : `claude mcp add failed: ${String(error)}`,
    );
    return;
  }
  const folder = vscode.workspace.workspaceFolders?.find((each) => each.uri.scheme === "file");
  if (!folder) {
    void vscode.window.showInformationMessage("Added Spelunk to Claude Code.");
    return;
  }
  const note = "Add Note";
  const choice = await vscode.window.showInformationMessage(
    `Added Spelunk to Claude Code. Also add a short note to ${folder.name}'s CLAUDE.md telling Claude when to use Spelunk? Without it, Claude tends to use grep.`,
    note,
    "Not Now",
  );
  if (choice !== note) return;
  try {
    const added = addClaudeNote(folder.uri.fsPath);
    void vscode.window.showInformationMessage(
      added ? `Added the note to ${folder.name}'s CLAUDE.md.` : `${folder.name}'s CLAUDE.md already has the note.`,
    );
  } catch (error) {
    void vscode.window.showErrorMessage(`Couldn't update CLAUDE.md: ${String(error)}`);
  }
}

/** Runs a program, resolving when it succeeds. */
function run(program: string, args: string[]): Promise<void> {
  return new Promise((resolve, reject) => {
    execFile(program, args, { timeout: 30_000 }, (error, _stdout, stderr) => {
      if (error) reject(stderr.trim() ? new Error(stderr.trim()) : error);
      else resolve();
    });
  });
}
