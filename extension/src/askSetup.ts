// How Ask is set up in VS Code: the spelunk.ask.* settings, the Anthropic
// API key in secret storage, and the command that sets it.
import * as vscode from "vscode";
import { type AskSetup, anthropicProvider, fakeProviderFromEnvironment } from "./ask";
import { askSettings } from "./settings";

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
  return { kind: "ready", provider: anthropicProvider({ apiKey, model: settings.model }) };
}

/**
 * Spelunk: Set Anthropic API Key. Asks for the key, keeps it in secret
 * storage, and turns Ask on when it was off: setting the key is asking
 * for it. An empty key removes the one kept.
 */
export async function setAnthropicApiKey(context: vscode.ExtensionContext): Promise<void> {
  const apiKey = await vscode.window.showInputBox({
    title: "Anthropic API key for Spelunk's Ask",
    prompt: "Kept in VS Code's secret storage on this computer. Only your questions go to Anthropic, never your code.",
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
