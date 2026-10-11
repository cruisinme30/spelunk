// What ⌘T, Go to Definition and Find References ask the daemon, and the
// answers, against the real daemon on the editor-navigation mock's files.
import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, relative } from "node:path";
import { test } from "node:test";
import type { Daemon } from "../daemon";
import { answers, asksOthersFirst, definitions, references, referencesQuery, workspaceSymbols } from "../navigation";
import type { OpenTarget, SymbolMatch } from "../protocol.gen";
import type { NavigationSettings } from "../settings";
import { newTestDaemon, SKIP_WITHOUT_DAEMON, untilIndexed } from "./realDaemon";

const DEFAULTS: NavigationSettings = { workspaceSymbols: true, definitions: "off", includeGeneratedAndVendored: false };

const FILES: Record<string, string> = {
  "payments-api/src/payments/retry_policy.py":
    "class RetryPolicy:\n    pass\n\n\ndef build():\n    return RetryPolicy()\n",
  "payments-api/third_party/tenacity/retry.py": "class RetryPolicy(retry_base):\n    pass\n",
  "shared-libs/go/retry/retry.go":
    "package retry\n\n// RetryPolicy says how often to retry.\ntype RetryPolicy struct {\n\tMaxAttempts int\n}\n\n" +
    "func NewRetryPolicy() RetryPolicy {\n\treturn RetryPolicy{MaxAttempts: 3}\n}\n",
  "shared-libs/go/retry/client.go": "package retry\n\nfunc Do(policy RetryPolicy) {}\n",
  "shared-libs/go/retry/policy_gen.go": "package retry\n\nvar defaults = RetryPolicy{}\n",
  "web-checkout/src/api/policy.ts": "export interface RetryPolicy {\n  attempts: number;\n}\n",
};

/** Writes the mock's three repos and starts a daemon on them, indexed. */
async function navigationDaemon(): Promise<{ daemon: Daemon; root: string }> {
  const root = mkdtempSync(join(tmpdir(), "us-nav-"));
  for (const [path, text] of Object.entries(FILES)) {
    mkdirSync(dirname(join(root, path)), { recursive: true });
    writeFileSync(join(root, path), text);
  }
  const daemon = newTestDaemon(
    ["payments-api", "shared-libs", "web-checkout"].map((name) => ({ id: name, path: join(root, name), name })),
  );
  await daemon.start();
  await untilIndexed(daemon);
  return { daemon, root };
}

function named(root: string, matches: SymbolMatch[]): string[] {
  return matches.map((m) => `${m.name} ${relative(root, m.path)}:${String(m.line)}:${String(m.column)}`);
}

function at(root: string, targets: OpenTarget[]): string[] {
  return targets.map((t) => `${relative(root, t.path ?? "")}:${String(t.line)}:${String(t.column)}`).sort();
}

test(
  "⌘T lists the definitions whose names hold the letters typed, in every repo and language",
  {
    skip: SKIP_WITHOUT_DAEMON,
  },
  async () => {
    // @covers screen:editor-navigation setting:editor.workspaceSymbols rpc:symbols/find
    // cspell:ignore retpol
    const { daemon, root } = await navigationDaemon();
    try {
      assert.deepEqual(named(root, await workspaceSymbols(daemon, "retpol", DEFAULTS)), [
        "RetryPolicy payments-api/src/payments/retry_policy.py:1:7",
        "RetryPolicy shared-libs/go/retry/retry.go:4:6",
        "RetryPolicy web-checkout/src/api/policy.ts:1:18",
        "NewRetryPolicy shared-libs/go/retry/retry.go:8:6",
      ]);
      assert.deepEqual(await workspaceSymbols(daemon, "  ", DEFAULTS), [], "blank text lists nothing");
      assert.deepEqual(
        await workspaceSymbols(daemon, "retpol", { ...DEFAULTS, workspaceSymbols: false }),
        [],
        "the setting turns it off",
      );
    } finally {
      await daemon.stop();
    }
  },
);

test(
  "generated and vendored definitions come only when the setting asks, marked and last",
  {
    skip: SKIP_WITHOUT_DAEMON,
  },
  async () => {
    // @covers setting:editor.includeGeneratedAndVendored
    const { daemon, root } = await navigationDaemon();
    try {
      const all = await workspaceSymbols(daemon, "RetryPolicy", { ...DEFAULTS, includeGeneratedAndVendored: true });
      // The vendored copy comes after the other definitions of the same name.
      assert.deepEqual(
        all.map((m) => `${m.name} ${relative(root, m.path)}${m.rankReason ? ` ${m.rankReason}` : ""}`),
        [
          "RetryPolicy payments-api/src/payments/retry_policy.py",
          "RetryPolicy shared-libs/go/retry/retry.go",
          "RetryPolicy web-checkout/src/api/policy.ts",
          "RetryPolicy payments-api/third_party/tenacity/retry.py vendored",
          "NewRetryPolicy shared-libs/go/retry/retry.go",
        ],
      );
    } finally {
      await daemon.stop();
    }
  },
);

test(
  "Go to Definition answers with the name's definitions in the file's language",
  {
    skip: SKIP_WITHOUT_DAEMON,
  },
  async () => {
    // @covers setting:editor.definitions
    const { daemon, root } = await navigationDaemon();
    try {
      const fromGo = join(root, "shared-libs/go/retry/client.go");
      assert.deepEqual(named(root, await definitions(daemon, { name: "RetryPolicy", path: fromGo }, DEFAULTS)), [
        "RetryPolicy shared-libs/go/retry/retry.go:4:6",
      ]);
      // A file in a language with no RetryPolicy gets every language's.
      const fromShell = join(root, "payments-api/deploy.sh");
      assert.equal((await definitions(daemon, { name: "RetryPolicy", path: fromShell }, DEFAULTS)).length, 3);
      // The name must match exactly, case and all.
      assert.deepEqual(await definitions(daemon, { name: "retryPolicy", path: fromGo }, DEFAULTS), []);
    } finally {
      await daemon.stop();
    }
  },
);

test(
  "Find References lists the lines using the name in the file's language, at the name",
  {
    skip: SKIP_WITHOUT_DAEMON,
  },
  async () => {
    // @covers setting:editor.definitions
    const { daemon, root } = await navigationDaemon();
    try {
      const fromGo = join(root, "shared-libs/go/retry/client.go");
      // Not the line that defines it, nor the generated file; the comment
      // counts, since a use is found by its name.
      assert.deepEqual(at(root, await references(daemon, { name: "RetryPolicy", path: fromGo }, DEFAULTS)), [
        "shared-libs/go/retry/client.go:3:16",
        "shared-libs/go/retry/retry.go:3:4",
        "shared-libs/go/retry/retry.go:8:23",
        "shared-libs/go/retry/retry.go:9:9",
      ]);
      const withGenerated = await references(
        daemon,
        { name: "RetryPolicy", path: fromGo },
        {
          ...DEFAULTS,
          includeGeneratedAndVendored: true,
        },
      );
      assert.ok(at(root, withGenerated).includes("shared-libs/go/retry/policy_gen.go:3:16"));
      // From a language without the name, every language's uses.
      const fromShell = join(root, "payments-api/deploy.sh");
      assert.ok(
        at(root, await references(daemon, { name: "RetryPolicy", path: fromShell }, DEFAULTS)).includes(
          "payments-api/src/payments/retry_policy.py:6:12",
        ),
      );
      assert.deepEqual(
        await references(daemon, { name: 'Retry"Policy', path: fromGo }, DEFAULTS),
        [],
        "a name ref: can't take finds nothing",
      );
    } finally {
      await daemon.stop();
    }
  },
);

test("spelunk.editor.definitions decides when Spelunk answers beside the language servers", () => {
  // @covers setting:editor.definitions
  const off = DEFAULTS;
  const fallback: NavigationSettings = { ...DEFAULTS, definitions: "fallback" };
  const always: NavigationSettings = { ...DEFAULTS, definitions: "always" };
  assert.deepEqual([answers(off, false), answers(off, true)], [false, false]);
  assert.deepEqual([answers(fallback, false), answers(fallback, true)], [true, false]);
  assert.deepEqual([answers(always, false), answers(always, true)], [true, true]);
  assert.deepEqual([asksOthersFirst(off), asksOthersFirst(fallback), asksOthersFirst(always)], [false, true, false]);
});

test("the references query takes code names only, and keeps to a language when given one", () => {
  assert.equal(
    referencesQuery("RetryPolicy", "go", DEFAULTS),
    'ref:"RetryPolicy" case:yes count:200 lang:go -is:generated -is:vendored',
  );
  assert.equal(
    referencesQuery("naïve_$x", undefined, { ...DEFAULTS, includeGeneratedAndVendored: true }),
    'ref:"naïve_$x" case:yes count:200',
  );
  for (const name of ['a"b', "a b", "a:b", ""]) assert.equal(referencesQuery(name, "go", DEFAULTS), undefined, name);
});
