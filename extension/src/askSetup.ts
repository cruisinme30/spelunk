// How Ask is set up in VS Code: the spelunk.ask.* settings, the Anthropic
// API key in secret storage, the command that sets it, and the command that
// picks the model and effort from what Anthropic lists for the key.
import Anthropic from "@anthropic-ai/sdk";
import * as vscode from "vscode";
import { type AskSetup, anthropicProvider, fakeProviderFromEnvironment } from "./ask";
import { ASK_EFFORTS, type AskEffort, askSettings } from "./settings";

/** Where the Anthropic API key is kept: VS Code's secret storage, never a settings file. */
const ANTHROPIC_KEY_SECRET = "spelunk.ask.anthropicApiKey";

const configuration = () => vscode.workspace.getConfiguration("spelunk");

/**
 * How Ask is set up right now: off in settings, no API key yet, or ready
 * with Anthropic. A test build may fake the provider with SPELUNK_ASK_FAKE.
 */
export async function askSetup(context: vscode.ExtensionContext): Promise<AskSetup> {
  const settings = askSettings(configuration());
  if (settings.provider === "off") return { kind: "off" };
  const fake = context.extensionMode === vscode.ExtensionMode.Production ? undefined : fakeProviderFromEnvironment();
  if (fake) return { kind: "ready", provider: fake };
  const apiKey = await context.secrets.get(ANTHROPIC_KEY_SECRET);
  if (!apiKey) return { kind: "noKey" };
  return { kind: "ready", provider: anthropicProvider({ apiKey, model: settings.model, effort: settings.effort }) };
}

/**
 * Spelunk: Set or Change Anthropic API Key. Asks for the key, keeps it in secret
 * storage, and turns Ask on when it was off: setting the key is asking
 * for it. An empty key removes the one kept.
 */
export async function setAnthropicApiKey(context: vscode.ExtensionContext): Promise<void> {
  const saved = await context.secrets.get(ANTHROPIC_KEY_SECRET);
  const apiKey = await vscode.window.showInputBox({
    title: saved ? "Change the Anthropic API key for Spelunk's Ask" : "Anthropic API key for Spelunk's Ask",
    prompt: saved
      ? `A key ending in …${saved.slice(-4)} is saved. Enter a new one to replace it, or leave this empty to remove it.`
      : "Kept in VS Code's secret storage on this computer. Only your questions go to Anthropic, never your code.",
    placeHolder: "sk-ant-…",
    password: true,
    ignoreFocusOut: true,
  });
  if (apiKey === undefined) return;
  if (apiKey.trim() === "") {
    await context.secrets.delete(ANTHROPIC_KEY_SECRET);
    void vscode.window.showInformationMessage("Removed the Anthropic API key.");
    return;
  }
  await context.secrets.store(ANTHROPIC_KEY_SECRET, apiKey.trim());
  const turnedOn = askSettings(configuration()).provider === "off";
  if (turnedOn) await configuration().update("ask.provider", "anthropic", vscode.ConfigurationTarget.Global);
  void vscode.window.showInformationMessage(
    turnedOn ? "Saved the key and turned Ask on: end a question with ? or press Ask." : "Saved the Anthropic API key.",
  );
}

/** Ask's commands: Set or Change Anthropic API Key and Choose Ask Model. */
export function askCommands(context: vscode.ExtensionContext): vscode.Disposable[] {
  return [
    vscode.commands.registerCommand("spelunk.setAnthropicApiKey", () => setAnthropicApiKey(context)),
    vscode.commands.registerCommand("spelunk.chooseAskModel", () => chooseAskModel(context)),
  ];
}

/** A model Anthropic lists for the key, as the picker shows it. */
interface ModelPick extends vscode.QuickPickItem {
  id: string;
  levels: AskEffort[];
}

/** The effort levels a listed model takes, lowest first; none for Claude 4.5 and older. */
function levelsOf(info: Anthropic.ModelInfo): AskEffort[] {
  const effort = info.capabilities?.effort;
  return effort ? ASK_EFFORTS.filter((level) => effort[level].supported) : [];
}

/** The models the key can use, newest first, with the levels each takes. Throws when Anthropic can't be asked. */
async function listModels(client: Pick<Anthropic, "models">, current: string): Promise<ModelPick[]> {
  const picks: ModelPick[] = [];
  for await (const info of client.models.list({ limit: 100 })) {
    if (info.lifecycle === "retired") continue;
    const levels = levelsOf(info);
    const notes = [
      info.id === current ? "current" : "",
      info.lifecycle === "deprecated" ? "deprecated" : "",
      levels.length > 0 ? `effort: ${levels.join(", ")}` : "no effort setting",
    ].filter((note) => note !== "");
    picks.push({ id: info.id, levels, label: info.display_name, description: info.id, detail: notes.join(" · ") });
  }
  return picks;
}

/**
 * Spelunk: Choose Ask Model. Lists the models Anthropic offers the key,
 * sets spelunk.ask.model to the one picked, then offers that model's
 * effort levels for spelunk.ask.effort. Without a key, asks for it first.
 */
async function chooseAskModel(context: vscode.ExtensionContext): Promise<void> {
  const apiKey = await context.secrets.get(ANTHROPIC_KEY_SECRET);
  if (!apiKey) {
    await setAnthropicApiKey(context);
    if (!(await context.secrets.get(ANTHROPIC_KEY_SECRET))) return;
    return chooseAskModel(context);
  }
  const settings = askSettings(configuration());
  const client = new Anthropic({ apiKey, maxRetries: 1 });
  let picks: ModelPick[];
  try {
    picks = await listModels(client, settings.model);
  } catch (error) {
    void vscode.window.showErrorMessage(
      `Couldn't list Anthropic's models: ${error instanceof Error ? error.message : String(error)}`,
    );
    return;
  }
  const model = await vscode.window.showQuickPick(picks, {
    title: "The model that turns a question into a Spelunk query",
    placeHolder: `Now ${settings.model}`,
    matchOnDescription: true,
  });
  if (!model) return;
  await configuration().update("ask.model", model.id, vscode.ConfigurationTarget.Global);
  if (model.levels.length === 0) {
    void vscode.window.showInformationMessage(`Ask now uses ${model.label}; it has no effort setting.`);
    return;
  }
  const effort = await vscode.window.showQuickPick(
    model.levels.map((level) => ({ label: level, description: level === settings.effort ? "current" : "" })),
    { title: `How long ${model.label} thinks about a question`, placeHolder: `Now ${settings.effort}` },
  );
  if (effort) await configuration().update("ask.effort", effort.label, vscode.ConfigurationTarget.Global);
  void vscode.window.showInformationMessage(
    `Ask now uses ${model.label} at ${effort?.label ?? settings.effort} effort.`,
  );
}
