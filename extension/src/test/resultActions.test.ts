// The result menu's actions: copying a result, its permalink (or why it has
// none), every result, an export, and a link to the search.
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdirSync, mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import type { Link, LinkParams, Remote, ResultItem, Root } from "../protocol.gen";
import {
  type ActionsSource,
  type ActionsUi,
  queryOfSearchLink,
  ResultActions,
  searchLink,
  ShownResults,
} from "../resultActions";
import type { ExportFormat } from "../resultText";
import { newTestDaemon, SKIP_WITHOUT_DAEMON, untilIndexed } from "./realDaemon";

const SHA = "4421bd636aec0b0706c0789e8894e475746c561b";
const GITHUB: Remote = { name: "origin", url: "git@github.com:acme/payments-api.git" };
const ROOT: Root = { id: "r1", path: "/work/payments-api", name: "payments-api" };
const LINE: ResultItem = {
  kind: "line",
  ref: "l1",
  repoId: "r1",
  path: "src/payments/client.py",
  line: 46,
  text: "for attempt in range(3):",
  hits: [],
};
const COMMIT: ResultItem = {
  kind: "commit",
  ref: "c1",
  repoId: "r1",
  sha: SHA,
  subject: "Retry charges",
  author: { name: "Jane Doe", email: "jane@acme.dev" },
  at: "2026-10-01T15:30:00Z",
  files: [],
  diffHits: 1,
  matchedTerms: [0],
  subjectHits: [],
  inMessage: true,
};

/** A Ui that records what was copied, opened, said, picked and saved. */
function recordingUi(pick?: string) {
  const copied: { text: string; done: string }[] = [];
  const opened: string[] = [];
  const said: { message: string; settings: boolean }[] = [];
  const offered: Remote[][] = [];
  const saved: { name: string; format: ExportFormat; text: string }[] = [];
  const ui: ActionsUi = {
    copy: (text, done) => {
      copied.push({ text, done });
      return Promise.resolve();
    },
    openExternal: (url) => {
      opened.push(url);
      return Promise.resolve();
    },
    inform: (message, settings = false) => void said.push({ message, settings }),
    pickRemote: (remotes) => {
      offered.push(remotes);
      return Promise.resolve(pick);
    },
    save: async (name, format, text) => {
      saved.push({ name, format, text: await text() });
      return true;
    },
  };
  return { ui, copied, opened, said, offered, saved };
}

/** A source over `items`, whose link/resolve answers each ref with `answer`. */
function sourceOf(
  items: ResultItem[],
  answer: (ref: string, params: LinkParams) => Link,
  extra: Partial<ActionsSource> = {},
) {
  const asked: LinkParams[] = [];
  const noSelection: { ref?: string } = {};
  const source: ActionsSource = {
    backend: {
      request: (method: string, params: LinkParams) => {
        assert.equal(method, "link/resolve");
        asked.push(params);
        return Promise.resolve({ links: params.refs.map((ref) => answer(ref, params)) });
      },
    },
    item: (ref) => items.find((item) => item.ref === ref),
    selectedRef: () => noSelection.ref,
    root: (repoId) => (repoId === ROOT.id ? ROOT : undefined),
    hosts: () => ({}),
    query: () => "retry_policy",
    shown: () => ({ items, total: items.length }),
    all: () => Promise.resolve(items),
    searchLink: (query) => searchLink("vscode", "spelunk.spelunk", query),
    separator: "/",
    ...extra,
  };
  return { source, asked };
}

test("Copy GitHub Permalink copies the link to the result's line; Open on GitHub opens it", async () => {
  // @covers command:result.copyPermalink.github command:result.openOnRemote.github command:result.copyPermalink command:result.openOnRemote
  const { source } = sourceOf([LINE, COMMIT], (ref) =>
    ref === "l1"
      ? { remote: GITHUB, commit: SHA, path: "src/payments/client.py", line: 44 }
      : { remote: GITHUB, commit: SHA },
  );
  const host = recordingUi();
  const actions = new ResultActions(source, host.ui);
  await actions.copyPermalink("l1");
  await actions.openOnRemote("c1");
  assert.deepEqual(host.copied, [
    {
      text: `https://github.com/acme/payments-api/blob/${SHA}/src/payments/client.py#L44`,
      done: "Copied a GitHub permalink to line 46",
    },
  ]);
  assert.deepEqual(host.opened, [`https://github.com/acme/payments-api/commit/${SHA}`]);
});

test("each kind of host's items copy that host's link", async () => {
  // @covers command:result.copyPermalink.gitlab command:result.openOnRemote.gitlab command:result.copyPermalink.bitbucket command:result.openOnRemote.bitbucket command:result.copyPermalink.gitea command:result.openOnRemote.gitea command:result.copyPermalink.azure command:result.openOnRemote.azure
  for (const [url, start] of [
    ["https://gitlab.com/acme/web/checkout.git", "https://gitlab.com/acme/web/checkout/-/blob/"],
    ["git@bitbucket.org:acme/api.git", "https://bitbucket.org/acme/api/src/"],
    ["https://codeberg.org/acme/api.git", "https://codeberg.org/acme/api/src/commit/"],
    ["git@ssh.dev.azure.com:v3/acme/payments/api", "https://dev.azure.com/acme/payments/_git/api?path="],
  ] as const) {
    const { source } = sourceOf([LINE], () => ({
      remote: { name: "origin", url },
      commit: SHA,
      path: LINE.path,
      line: 46,
    }));
    const host = recordingUi();
    const actions = new ResultActions(source, host.ui);
    await actions.copyPermalink("l1");
    await actions.openOnRemote("l1");
    assert.ok(host.copied[0]?.text.startsWith(start), host.copied[0]?.text);
    assert.equal(host.opened[0], host.copied[0]?.text);
  }
});

test("a result without a permalink says why, and an unknown host offers its setting", async () => {
  const cases: [Link, string, boolean][] = [
    [{ remote: GITHUB, problem: "lineChanged" }, "Line 46 has changes GitHub doesn't have yet.", false],
    [{ problem: "noRemote" }, "payments-api has no Git remote", false],
    [
      { remote: { name: "origin", url: "git@git.acme.corp:a/b.git" }, commit: SHA, path: LINE.path, line: 46 },
      "git.acme.corp",
      true,
    ],
  ];
  for (const [link, says, settings] of cases) {
    const { source } = sourceOf([LINE], () => link);
    const host = recordingUi();
    await new ResultActions(source, host.ui).copyPermalink("l1");
    assert.deepEqual(host.copied, []);
    assert.ok(host.said[0]?.message.includes(says), host.said[0]?.message);
    assert.equal(host.said[0]?.settings, settings);
  }
});

test("with several remotes and none tracked, the user picks one; cancelling copies nothing", async () => {
  const fork: Remote = { name: "fork", url: "git@github.com:dana/payments-api.git" };
  const answer = (_ref: string, params: LinkParams): Link =>
    params.remote === undefined
      ? { problem: "pickRemote", remotes: [GITHUB, fork] }
      : { remote: fork, commit: SHA, path: LINE.path, line: 46 };
  const picked = recordingUi("fork");
  const { source, asked } = sourceOf([LINE], answer);
  await new ResultActions(source, picked.ui).copyPermalink("l1");
  assert.deepEqual(picked.offered, [[GITHUB, fork]]);
  assert.deepEqual(asked.at(-1), { refs: ["l1"], remote: "fork" });
  assert.ok(picked.copied[0]?.text.startsWith("https://github.com/dana/payments-api/blob/"));
  const cancelled = recordingUi();
  await new ResultActions(sourceOf([LINE], answer).source, cancelled.ui).copyPermalink("l1");
  assert.deepEqual(cancelled.copied, []);
  assert.deepEqual(cancelled.said, []);
});

test("copying acts on the clicked result, or from a key on the selected one", async () => {
  // @covers command:result.copyPath command:result.copyRelativePath command:result.copySha
  const { source } = sourceOf([LINE, COMMIT], () => ({ problem: "noRemote" }), { selectedRef: () => "c1" });
  const host = recordingUi();
  const actions = new ResultActions(source, host.ui);
  await actions.copyPath("l1");
  await actions.copyRelativePath("l1");
  await actions.copySha();
  await actions.copyPath(); // the selected result is a commit, which has no path
  assert.deepEqual(
    host.copied.map((copy) => copy.text),
    ["/work/payments-api/src/payments/client.py:46", "src/payments/client.py:46", SHA],
  );
});

test("Copy All copies what's loaded and says when the search found more; Export saves everything with links", async () => {
  // @covers command:results.copyAll command:results.exportMarkdown command:results.exportText command:results.copySearchLink
  const second: ResultItem = { ...LINE, ref: "l2", line: 50, text: "delay = policy.next()" };
  const { source, asked } = sourceOf(
    [LINE],
    (ref) => ({ remote: GITHUB, commit: SHA, path: LINE.path, line: ref === "l1" ? 46 : 50 }),
    { shown: () => ({ items: [LINE], total: 2 }), all: () => Promise.resolve([LINE, second]) },
  );
  const host = recordingUi();
  const actions = new ResultActions(source, host.ui);
  await actions.copyAll();
  const [copiedAll] = host.copied;
  assert.ok(copiedAll);
  assert.equal(copiedAll.done, "Copied the 1 shown of 2 results as text");
  assert.equal(
    copiedAll.text,
    "# Spelunk: retry_policy\npayments-api/src/payments/client.py:46: for attempt in range(3):\n",
  );
  await actions.export("md");
  const [markdown] = host.saved;
  assert.ok(markdown);
  assert.equal(markdown.name, "spelunk-retry_policy.md");
  assert.match(markdown.text, /\[L50\]\(https:\/\/github\.com\/acme\/payments-api\/blob\/.*#L50\)/u);
  assert.deepEqual(asked.at(-1), { refs: ["l1", "l2"] });
  // Text needs no links, so it asks for none.
  await actions.export("txt");
  assert.equal(asked.length, 1);
  await actions.copySearchLink();
  assert.equal(host.copied.at(-1)?.text, "vscode://spelunk.spelunk/search?q=retry_policy");
});

test("a search link round-trips any query through VS Code's decoding", () => {
  for (const query of ["retry_policy", 'author:jane "retry later" f:^src/a+b/ x&y=1', "#TODO 50%"]) {
    const link = new URL(searchLink("vscode", "spelunk.spelunk", query));
    assert.equal(queryOfSearchLink(link.pathname, decodeURIComponent(link.search.slice(1))), query);
  }
  assert.equal(queryOfSearchLink("/other", "q=x"), undefined);
  assert.equal(queryOfSearchLink("/search", "q="), undefined);
  assert.equal(searchLink("cursor", "spelunk.spelunk", "a b"), "cursor://spelunk.spelunk/search?q=a%20b");
});

/** Runs git in `root`, as Dana. */
function git(root: string, ...args: string[]): string {
  return execFileSync("git", ["-C", root, ...args], {
    encoding: "utf8",
    env: {
      ...process.env,
      GIT_AUTHOR_NAME: "Dana",
      GIT_AUTHOR_EMAIL: "d@acme.dev",
      GIT_COMMITTER_NAME: "Dana",
      GIT_COMMITTER_EMAIL: "d@acme.dev",
    },
  }).trim();
}

test(
  "with the real daemon: a line moved by uncommitted edits links to its line in the pushed commit",
  { skip: SKIP_WITHOUT_DAEMON },
  async () => {
    // @covers command:result.copyPermalink.github
    const root = mkdtempSync(join(tmpdir(), "us-link-"));
    mkdirSync(join(root, "src"));
    writeFileSync(join(root, "src/client.py"), "import time\n\ndef charge():\n    retry_policy = 3\n");
    git(root, "init", "--quiet", "-b", "main");
    git(root, "add", "-A");
    git(root, "commit", "--quiet", "-m", "Add the client");
    git(root, "remote", "add", "origin", GITHUB.url);
    git(root, "update-ref", "refs/remotes/origin/main", "HEAD");
    const head = git(root, "rev-parse", "HEAD");
    writeFileSync(join(root, "src/client.py"), "# new\n# lines\nimport time\n\ndef charge():\n    retry_policy = 3\n");
    const daemon = newTestDaemon([{ id: "r1", path: root, name: "payments-api" }]);
    await daemon.start();
    try {
      await untilIndexed(daemon);
      const found: ResultItem[] = [];
      daemon.on("batch", (batch) => found.push(...batch.items));
      await daemon.request("search/start", { searchId: "s1", text: "retry_policy = 3" });
      const line = found.find((item) => item.kind === "line");
      assert.ok(line?.line === 6, JSON.stringify(found));
      const host = recordingUi();
      const actions = new ResultActions(
        {
          ...sourceOf(found, () => ({ problem: "noRemote" })).source,
          backend: daemon,
          item: (ref) => found.find((item) => item.ref === ref),
        },
        host.ui,
      );
      await actions.copyPermalink(line.ref);
      assert.deepEqual(host.copied, [
        {
          text: `https://github.com/acme/payments-api/blob/${head}/src/client.py#L4`,
          done: "Copied a GitHub permalink to line 6",
        },
      ]);
    } finally {
      await daemon.stop();
    }
  },
);

test("what the menu knows of the panel comes from what the panel is sent", () => {
  let restores = 0;
  const shown = new ShownResults();
  shown.onRestore = () => {
    restores++;
  };
  shown.posted("preview.result", { ref: "l1", preview: null });
  shown.posted("search.done", { seq: 1, searchId: "s1", total: 120, truncated: false, hidden: [], ms: 3 });
  shown.posted("state.restore", { recent: [], pinned: [] });
  shown.posted("index.status", { repos: [] });
  assert.deepEqual([shown.selectedRef, shown.total, restores], ["l1", 120, 1]);
});
