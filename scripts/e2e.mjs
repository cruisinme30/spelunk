#!/usr/bin/env node
// Runs the end-to-end tests (extension/src/e2e) in a real VS Code window on
// the fixture workspace. It downloads the latest stable VS Code once into
// .vscode-test/, bundles the tests, and starts VS Code with the extension
// under development and SPELUNK_TEST=1. The exit code is VS Code's:
// 0 when every test passed.
//
// Usage: node scripts/e2e.mjs    (on Linux without a display: xvfb-run -a node scripts/e2e.mjs)
//
// Needs the daemon binary (npm test builds it) and network access to
// update.code.visualstudio.com the first time.
import { execFileSync, spawn } from "node:child_process";
import { existsSync, mkdirSync, mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = join(dirname(fileURLToPath(import.meta.url)), "..");
const cache = join(repoRoot, ".vscode-test");

/** The download for this machine, and where its executable may be once unpacked. */
function platformBuild() {
  if (process.platform === "linux") {
    return { platform: "linux-x64", archive: "vscode.tar.gz", executables: ["VSCode-linux-x64/code"] };
  }
  if (process.platform === "darwin") {
    const platform = process.arch === "arm64" ? "darwin-arm64" : "darwin";
    // Older builds call the macOS executable Electron, newer ones Code.
    const app = "Visual Studio Code.app/Contents/MacOS";
    return { platform, archive: "vscode.zip", executables: [`${app}/Code`, `${app}/Electron`] };
  }
  if (process.platform === "win32") {
    return { platform: "win32-x64-archive", archive: "vscode.zip", executables: ["Code.exe"] };
  }
  throw new Error(`no VS Code build for ${process.platform}`);
}

/** The first of the build's executables that exists in folder, if any. */
function findExecutable(folder, build) {
  return build.executables.map((name) => join(folder, name)).find((path) => existsSync(path));
}

/** Downloads and unpacks VS Code into .vscode-test/<platform>, once. */
async function installVsCode() {
  const build = platformBuild();
  const folder = join(cache, build.platform);
  const installed = findExecutable(folder, build);
  if (installed) return installed;
  mkdirSync(folder, { recursive: true });
  const url = `https://update.code.visualstudio.com/latest/${build.platform}/stable`;
  console.log(`Downloading VS Code from ${url}`);
  const response = await fetch(url);
  if (!response.ok) throw new Error(`download failed: ${response.status} ${response.statusText}`);
  const archive = join(folder, build.archive);
  writeFileSync(archive, Buffer.from(await response.arrayBuffer()));
  if (build.archive.endsWith(".tar.gz")) execFileSync("tar", ["-xzf", archive, "-C", folder]);
  else if (process.platform === "win32") {
    execFileSync("powershell", [
      "-NoProfile",
      "-Command",
      `Expand-Archive -Path '${archive}' -DestinationPath '${folder}'`,
    ]);
  } else execFileSync("unzip", ["-q", archive, "-d", folder]);
  const executable = findExecutable(folder, build);
  if (!executable) throw new Error(`unpacked VS Code has none of ${build.executables.join(", ")}`);
  return executable;
}

const executable = await installVsCode();
execFileSync(process.execPath, [join(repoRoot, "extension/build.mjs"), "--e2e"], { stdio: "inherit" });

const scratch = mkdtempSync(join(tmpdir(), "us-e2e-"));
// VS Code must start as itself, not as Node, even when this runs under Electron.
const environment = { ...process.env, SPELUNK_TEST: "1" };
delete environment.ELECTRON_RUN_AS_NODE;
const child = spawn(
  executable,
  [
    join(repoRoot, "testdata/workspace"),
    `--extensionDevelopmentPath=${join(repoRoot, "extension")}`,
    `--extensionTestsPath=${join(repoRoot, "extension/dist-e2e/index.js")}`,
    `--user-data-dir=${join(scratch, "user")}`,
    `--extensions-dir=${join(scratch, "extensions")}`,
    "--disable-workspace-trust",
    "--skip-welcome",
    "--skip-release-notes",
    "--no-sandbox",
  ],
  { stdio: "inherit", env: environment },
);
child.on("exit", (code) => {
  process.exitCode = code ?? 1;
});
