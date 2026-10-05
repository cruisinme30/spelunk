// Packages the extension as a .vsix for one platform: builds the daemon for
// that platform (Go cross-compiles, so any platform can be built anywhere),
// bundles the extension and webview, and writes out/spelunk-<version>-<target>.vsix.
//
// Usage:
//   npm run package                          (this machine's platform)
//   npm run package -- --target linux-x64    (another one)
//
// Install the result with: code --install-extension out/spelunk-<version>-<target>.vsix
import { createVSIX } from "@vscode/vsce";
import { execFileSync } from "node:child_process";
import { copyFileSync, mkdirSync, readFileSync, rmSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = join(dirname(fileURLToPath(import.meta.url)), "..");
const extensionRoot = join(repoRoot, "extension");

/** VS Code's platform targets, with the Go platform each one's daemon is built for. */
const TARGETS = {
  "darwin-arm64": { goos: "darwin", goarch: "arm64" },
  "darwin-x64": { goos: "darwin", goarch: "amd64" },
  "linux-arm64": { goos: "linux", goarch: "arm64" },
  "linux-x64": { goos: "linux", goarch: "amd64" },
  "win32-arm64": { goos: "windows", goarch: "arm64" },
  "win32-x64": { goos: "windows", goarch: "amd64" },
};

const targetFlag = process.argv.indexOf("--target");
const target = targetFlag === -1 ? `${process.platform}-${process.arch}` : process.argv[targetFlag + 1];
const platform = target === undefined ? undefined : TARGETS[target];
if (platform === undefined) {
  throw new Error(`Unknown target ${String(target)}. Pick one of: ${Object.keys(TARGETS).join(", ")}`);
}

const { version } = JSON.parse(readFileSync(join(extensionRoot, "package.json"), "utf8"));
const executable = platform.goos === "windows" ? "spelunk-daemon.exe" : "spelunk-daemon";

// The extension looks for its daemon in bin/<target>/ first (see daemonBinary in extension.ts).
const binDirectory = join(extensionRoot, "bin");
rmSync(binDirectory, { recursive: true, force: true });
mkdirSync(join(binDirectory, target), { recursive: true });
console.log(`Building the daemon for ${target}`);
execFileSync(
  "go",
  ["build", "-trimpath", "-ldflags=-s -w", "-o", join(binDirectory, target, executable), "./cmd/spelunk-daemon"],
  {
    cwd: join(repoRoot, "daemon"),
    stdio: "inherit",
    env: { ...process.env, GOOS: platform.goos, GOARCH: platform.goarch, CGO_ENABLED: "0" },
  },
);

console.log("Bundling the extension and webview");
execFileSync(process.execPath, [join(extensionRoot, "build.mjs")], { stdio: "inherit" });

// The package carries the repo's license; the copy is git-ignored.
copyFileSync(join(repoRoot, "LICENSE"), join(extensionRoot, "LICENSE"));

const packagePath = join(repoRoot, "out", `spelunk-${version}-${target}.vsix`);
mkdirSync(dirname(packagePath), { recursive: true });
try {
  // dependencies: false because esbuild already bundled everything the extension imports.
  await createVSIX({ cwd: extensionRoot, packagePath, target, dependencies: false });
} finally {
  // Left behind, this daemon would shadow daemon/bin's fresh builds in every later dev or e2e run.
  rmSync(binDirectory, { recursive: true, force: true });
}
console.log(`Install it with: code --install-extension ${packagePath}`);
