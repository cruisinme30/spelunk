// The spec items that live in the code: the query operators and the
// webview and RPC messages (protocol/protocol.schema.json), the settings and
// commands (extension/package.json) and the diagnostic codes
// (daemon/internal/query). specCoverage.mjs checks each has a test, and
// checkWikiSync.mjs that the wiki describes each.
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";

/** Reads a JSON file under `root`. */
const readJson = (root, path) => JSON.parse(readFileSync(join(root, path), "utf8"));

/**
 * The query operators' names, as the protocol spells them ("f", "lang", "author", ...).
 * @param {string} root The repository root.
 * @returns {string[]} The names.
 */
export function operatorNames(root) {
  return readJson(root, "protocol/protocol.schema.json").$defs.OpName.enum;
}

/**
 * The RPC methods and webview messages the protocol defines.
 * @param {string} root The repository root.
 * @returns {{rpc: string[], msg: string[]}} Their names.
 */
export function messageNames(root) {
  const schema = readJson(root, "protocol/protocol.schema.json");
  return { rpc: Object.keys(schema["x-rpc-methods"]), msg: Object.keys(schema["x-webview-messages"]) };
}

/**
 * The extension's settings, without the deprecated keys that only say where a setting moved.
 * @param {string} root The repository root.
 * @returns {string[]} Full keys, such as "spelunk.search.caseSensitive".
 */
export function settingKeys(root) {
  return readJson(root, "extension/package.json").contributes.configuration.flatMap((section) =>
    Object.entries(section.properties)
      .filter(([, property]) => !property.deprecationMessage)
      .map(([key]) => key),
  );
}

/**
 * The extension's commands.
 * @param {string} root The repository root.
 * @returns {{command: string, title: string}[]} Each command's id and title.
 */
export function commands(root) {
  return readJson(root, "extension/package.json").contributes.commands;
}

/**
 * The query diagnostics' codes, declared as `Diag… = "code"` constants in daemon/internal/query.
 * @param {string} root The repository root.
 * @returns {string[]} Codes such as "unclosed_paren".
 */
export function diagnosticCodes(root) {
  const queryPackage = join(root, "daemon/internal/query");
  let names;
  try {
    names = readdirSync(queryPackage);
  } catch {
    return []; // a checkout without the daemon
  }
  return names
    .filter((name) => name.endsWith(".go") && !name.endsWith("_test.go"))
    .flatMap((name) =>
      [...readFileSync(join(queryPackage, name), "utf8").matchAll(/^\s*Diag\w+\s*=\s*"([a-z_]+)"/gm)].map(
        (match) => match[1] ?? "",
      ),
    );
}
