// The query Search in Folder writes for the folders picked in the Explorer.
// The last test runs each one against the real daemon, to prove it finds
// the folder's files, and only those, whatever the folder is called.
// @covers screen:search-in-folder command:searchInFolder
import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { test } from "node:test";
import {
  FolderScopeNotes,
  joinPaths,
  pathLabel,
  type PickedPath,
  scopedPaths,
  scopeQuery,
  startsWithScope,
  withScope,
} from "../folderScope";
import type { Root } from "../protocol.gen";
import { makeRoot } from "../roots";
import { newTestDaemon, SKIP_WITHOUT_DAEMON as skip, untilIndexed } from "./realDaemon";

const WEB: Root = { id: "w", name: "web-checkout", path: join("/work", "web-checkout") };
const PAY: Root = { id: "p", name: "payments-api", path: join("/work", "payments-api") };
const HTTP: Root = { id: "h", name: "http", path: join("/work", "shared-libs", "http") };
const SHARED: Root = { id: "s", name: "shared-libs", path: join("/work", "shared-libs") };

/** A folder under `root`, as the Explorer passes it. */
const folder = (root: Root, ...segments: string[]): PickedPath => ({
  path: join(root.path, ...segments),
  isFolder: true,
});
const file = (root: Root, ...segments: string[]): PickedPath => ({
  path: join(root.path, ...segments),
  isFolder: false,
});

/** The query for `picked` among `roots`. */
function scope(picked: PickedPath[], roots: Root[]): string {
  return scopeQuery(joinPaths([], scopedPaths(picked, roots)), roots.length > 1);
}

test("a folder is anchored at its repo's root, with its repo named when more than one is open", () => {
  assert.equal(scope([folder(WEB, "src", "api")], [WEB]), "f:^src/api/");
  assert.equal(scope([folder(WEB, "src", "api")], [WEB, PAY]), "repo:^web-checkout$ f:^src/api/");
  assert.equal(scope([folder(WEB)], [WEB, PAY]), "repo:^web-checkout$", "a workspace folder is its whole repo");
  assert.equal(scope([folder(WEB)], [WEB]), "repo:^web-checkout$");
  assert.equal(scope([file(WEB, "package.json")], [WEB]), String.raw`f:^package\.json$`, "a file is its whole path");
});

test("a name with regex characters is escaped, and one with a space, bracket or quote takes the slash form", () => {
  assert.equal(scope([folder(WEB, "a.b")], [WEB]), String.raw`f:^a\.b/`);
  assert.equal(
    scope([folder(WEB, "c++", "x*", "y?", "$v", "{z}", "a|b", "^q")], [WEB]),
    String.raw`f:^c\+\+/x\*/y\?/\$v/\{z\}/a\|b/\^q/`,
  );
  assert.equal(
    scope([folder(WEB, "src", "app", "(shop)", "orders", "[id]")], [WEB]),
    String.raw`f:/^src\/app\/\(shop\)\/orders\/\[id\]\//`,
  );
  assert.equal(scope([folder(WEB, "src", "my components")], [WEB]), String.raw`f:/^src\/my components\//`);
  assert.equal(scope([folder(WEB, 'say "hi"')], [WEB]), String.raw`f:/^say "hi"\//`);
  const spaced: Root = { id: "m", name: "my repo (old)", path: join("/work", "my repo (old)") };
  assert.equal(scope([folder(spaced, "src")], [spaced, WEB]), String.raw`repo:/^my repo \(old\)$/ f:^src/`);
});

test("several folders are ORed, grouped by repo, each repo keeping its own folders", () => {
  assert.equal(scope([folder(WEB, "src", "api"), folder(WEB, "e2e")], [WEB]), "(f:^src/api/ OR f:^e2e/)");
  assert.equal(
    scope([folder(WEB, "src", "api"), folder(WEB, "e2e")], [WEB, PAY]),
    "repo:^web-checkout$ (f:^src/api/ OR f:^e2e/)",
  );
  assert.equal(
    scope([folder(PAY, "src"), folder(WEB, "src")], [WEB, PAY]),
    "(repo:^payments-api$ f:^src/ OR repo:^web-checkout$ f:^src/)",
  );
  assert.equal(
    scope([folder(PAY), folder(WEB, "src")], [WEB, PAY]),
    "(repo:^payments-api$ OR repo:^web-checkout$ f:^src/)",
  );
  assert.equal(
    scope([folder(WEB, "src"), file(WEB, "package.json")], [WEB, PAY]),
    String.raw`repo:^web-checkout$ (f:^src/ OR f:^package\.json$)`,
    "a file selected with folders is searched too",
  );
});

test("a folder inside another picked one adds nothing, in either order", () => {
  assert.equal(scope([folder(WEB, "src"), folder(WEB, "src", "api")], [WEB]), "f:^src/");
  assert.equal(scope([folder(WEB, "src", "api"), folder(WEB, "src")], [WEB]), "f:^src/");
  assert.equal(scope([folder(WEB, "src"), file(WEB, "src", "a.ts")], [WEB]), "f:^src/");
  assert.equal(scope([folder(WEB, "src"), folder(WEB, "src")], [WEB]), "f:^src/", "the same folder twice is once");
  assert.equal(scope([folder(WEB, "src"), folder(WEB)], [WEB, PAY]), "repo:^web-checkout$");
  assert.equal(
    scope([folder(WEB, "src"), folder(WEB, "source")], [WEB]),
    "(f:^src/ OR f:^source/)",
    "a sibling isn't inside",
  );
});

test("a folder that is a workspace folder of its own, inside another, searches that one", () => {
  const roots = [SHARED, HTTP];
  assert.equal(scope([{ path: HTTP.path, isFolder: true }], roots), "repo:^http$");
  assert.equal(scope([folder(HTTP, "tests")], roots), "repo:^http$ f:^tests/");
  assert.equal(scope([folder(SHARED, "docs")], roots), "repo:^shared-libs$ f:^docs/");
  assert.deepEqual(scopedPaths([{ path: join("/elsewhere", "x"), isFolder: true }], roots), [], "outside every root");
});

test("a second pick joins the first while the box starts with its scope; otherwise it goes in front", () => {
  const first = joinPaths([], scopedPaths([folder(PAY, "src")], [WEB, PAY]));
  const firstScope = scopeQuery(first, true);
  assert.equal(withScope("retry", firstScope), "repo:^payments-api$ f:^src/ retry");
  assert.equal(withScope("", firstScope), "repo:^payments-api$ f:^src/ ", "a space follows, to type into");
  const joined = joinPaths(first, scopedPaths([folder(WEB, "src", "api")], [WEB, PAY]));
  const secondScope = scopeQuery(joined, true);
  assert.equal(secondScope, "(repo:^payments-api$ f:^src/ OR repo:^web-checkout$ f:^src/api/)");
  assert.equal(
    withScope(`${firstScope} retry`, secondScope, firstScope),
    "(repo:^payments-api$ f:^src/ OR repo:^web-checkout$ f:^src/api/) retry",
    "the earlier scope is replaced, the search word stays",
  );
  assert.equal(
    withScope("author:jane retry -f:vendor/", firstScope),
    "repo:^payments-api$ f:^src/ author:jane retry -f:vendor/",
    "other filters stay",
  );
  assert.ok(startsWithScope(`${firstScope} retry`, firstScope));
  assert.ok(!startsWithScope(`${firstScope}x retry`, firstScope), "only as a whole term");
  assert.ok(!startsWithScope(`retry ${firstScope}`, firstScope));
});

test("the notes go on searches that start with the scope, and only those", () => {
  const notes = new FolderScopeNotes();
  const scope = "(repo:^web$ f:^dist/ OR repo:^pay$ f:^plugins/stripe/)";
  const folderNotes = [
    { folder: "web/dist/", skipped: "ignored" as const },
    { folder: "pay/plugins/stripe/", gitRepo: "pay/plugins/stripe/" },
  ];
  const done = { seq: 1, searchId: "s1", total: 0, truncated: false, hidden: [], ms: 0 };
  assert.deepEqual(notes.added("search.done", done, `${scope} retry`), done, "nothing before a pick");
  notes.set(scope, folderNotes);
  assert.equal(notes.current(`${scope} retry`), scope);
  assert.deepEqual(notes.added("search.done", done, `${scope} retry`), { ...done, folderNotes });
  assert.deepEqual(notes.added("search.done", done, "retry"), done, "with the scope gone, so are its notes");
  assert.deepEqual(notes.added("search.done", done, `retry ${scope}`), done, "only a scope at the front counts");
  assert.deepEqual(notes.added("search.done", done), done, "a search that didn't finish");
  assert.equal(notes.current("retry"), undefined);
  const batch = { seq: 1, searchId: "s1", items: [] };
  assert.equal(notes.added("search.batch", batch, `${scope} retry`), batch, "other messages pass as they are");
  notes.set(scope, []);
  assert.deepEqual(notes.added("search.done", done, `${scope} retry`), done, "no notes, no field");
});

test("notes name a folder by its path, and its repo too when more than one is open", () => {
  assert.equal(pathLabel({ root: WEB, path: "dist", isFolder: true }, false), "dist/");
  assert.equal(pathLabel({ root: WEB, path: "dist", isFolder: true }, true), "web-checkout/dist/");
  assert.equal(pathLabel({ root: WEB, path: "", isFolder: true }, true), "web-checkout/");
});

test("the daemon finds every scope's files, and only those, whatever the folders are called", { skip }, async () => {
  const workspace = mkdtempSync(join(tmpdir(), "us-folders-"));
  const files: Record<string, string[]> = {
    "web-checkout": [
      "src/api/checkout.ts",
      "e2e/src/api/flow.ts",
      "src/app/(shop)/orders/[id]/page.tsx",
      "src/my components/RetryBanner.tsx",
      "a.b/x.ts",
      "aXb/x.ts",
      "c++/x.cc",
      "star*/x.ts",
      "starXY/x.ts",
      "package.json",
    ],
    "payments-api": ["src/payments/retry.py", "README.md"],
  };
  for (const [repo, paths] of Object.entries(files)) {
    for (const path of paths) {
      mkdirSync(dirname(join(workspace, repo, path)), { recursive: true });
      writeFileSync(join(workspace, repo, path), "retry\n");
    }
  }
  const roots = Object.keys(files).map((name) => makeRoot(join(workspace, name)));
  const [web] = roots;
  assert.ok(web);
  const names = new Map(roots.map((root) => [root.id, root.name]));
  const daemon = newTestDaemon(roots);
  const found = new Map<string, string[]>();
  daemon.on("batch", ({ searchId, items }) => {
    const paths = found.get(searchId) ?? [];
    for (const item of items) if (item.kind === "file") paths.push(`${names.get(item.repoId) ?? "?"}/${item.path}`);
    found.set(searchId, paths);
  });
  await daemon.start();
  try {
    await untilIndexed(daemon);
    const cases: [picked: PickedPath[], want: string[]][] = [
      [[folder(web, "src", "api")], ["web-checkout/src/api/checkout.ts"]],
      [[folder(web, "src", "app", "(shop)", "orders", "[id]")], ["web-checkout/src/app/(shop)/orders/[id]/page.tsx"]],
      [[folder(web, "src", "my components")], ["web-checkout/src/my components/RetryBanner.tsx"]],
      [[folder(web, "a.b")], ["web-checkout/a.b/x.ts"]],
      [[folder(web, "c++")], ["web-checkout/c++/x.cc"]],
      [[folder(web, "star*")], ["web-checkout/star*/x.ts"]],
      [[file(web, "package.json")], ["web-checkout/package.json"]],
      [
        [folder(web, "src", "api"), { path: join(workspace, "payments-api", "src"), isFolder: true }],
        ["payments-api/src/payments/retry.py", "web-checkout/src/api/checkout.ts"],
      ],
      [
        [{ path: join(workspace, "payments-api"), isFolder: true }],
        ["payments-api/README.md", "payments-api/src/payments/retry.py"],
      ],
    ];
    for (const [index, [picked, want]] of cases.entries()) {
      const text = scope(picked, roots);
      const { query } = await daemon.request("query/parse", { text, cursor: text.length });
      assert.deepEqual(query.diagnostics, [], `${text}: diagnostics`);
      await daemon.request("search/start", { searchId: `f${index}`, text: `${text} type:file` });
      assert.deepEqual([...(found.get(`f${index}`) ?? [])].sort(), want, text);
    }
  } finally {
    await daemon.stop();
  }
});
