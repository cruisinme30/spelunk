// Permalinks as web addresses: reading remotes, each host's link format, and why a result has none.
import assert from "node:assert/strict";
import { test } from "node:test";
import { permalink, problemMessage, remotesState, repoHost, webRemote } from "../permalinks";
import type { Link } from "../protocol.gen";

const SHA = "4421bd636aec0b0706c0789e8894e475746c561b";

/** A link to src/payments/client.py at SHA, line 46, on a remote at `url`. */
function lineLink(url: string, extra: Partial<Link> = {}): Link {
  return { remote: { name: "origin", url }, commit: SHA, path: "src/payments/client.py", line: 46, ...extra };
}

/** The address permalink makes, failing if it makes none. */
function address(link: Link, hosts = {}): string {
  const made = permalink(link, hosts);
  assert.ok("url" in made, `no address: ${JSON.stringify(made)}`);
  return made.url;
}

test("a remote's URL reads as its website, in every form git writes it, never with credentials", () => {
  for (const url of [
    "git@github.com:acme/payments-api.git",
    "ssh://git@github.com/acme/payments-api.git",
    "ssh://git@ssh.github.com:443/acme/payments-api.git",
    "https://github.com/acme/payments-api.git",
    "https://github.com/acme/payments-api",
    "https://jane:ghp_token123@github.com/acme/payments-api.git",
    "git://github.com/acme/payments-api.git/",
  ]) {
    const remote = webRemote(url);
    assert.ok(remote, url);
    assert.equal(remote.web, "https://github.com/acme/payments-api", url);
    assert.equal(remote.kind, "github", url);
  }
  assert.equal(webRemote("/srv/git/payments.git"), undefined);
  assert.equal(webRemote("not a url"), undefined);
});

test("each kind of host links to a line, a range and a commit in its own format", () => {
  // @covers setting:permalinks.hosts
  const cases: [string, string, string, string][] = [
    [
      "git@github.com:acme/payments-api.git",
      `https://github.com/acme/payments-api/blob/${SHA}/src/payments/client.py#L46`,
      `https://github.com/acme/payments-api/blob/${SHA}/src/payments/client.py#L46-L48`,
      `https://github.com/acme/payments-api/commit/${SHA}`,
    ],
    [
      "https://gitlab.com/acme/web/checkout.git",
      `https://gitlab.com/acme/web/checkout/-/blob/${SHA}/src/payments/client.py#L46`,
      `https://gitlab.com/acme/web/checkout/-/blob/${SHA}/src/payments/client.py#L46-48`,
      `https://gitlab.com/acme/web/checkout/-/commit/${SHA}`,
    ],
    [
      "git@bitbucket.org:acme/payments-api.git",
      `https://bitbucket.org/acme/payments-api/src/${SHA}/src/payments/client.py#lines-46`,
      `https://bitbucket.org/acme/payments-api/src/${SHA}/src/payments/client.py#lines-46:48`,
      `https://bitbucket.org/acme/payments-api/commits/${SHA}`,
    ],
    [
      "https://codeberg.org/acme/payments-api.git",
      `https://codeberg.org/acme/payments-api/src/commit/${SHA}/src/payments/client.py#L46`,
      `https://codeberg.org/acme/payments-api/src/commit/${SHA}/src/payments/client.py#L46-L48`,
      `https://codeberg.org/acme/payments-api/commit/${SHA}`,
    ],
    [
      "git@ssh.dev.azure.com:v3/acme/payments/payments-api",
      `https://dev.azure.com/acme/payments/_git/payments-api?path=/src/payments/client.py&version=GC${SHA}&line=46&lineEnd=47&lineStartColumn=1&lineEndColumn=1&lineStyle=plain&_a=contents`,
      `https://dev.azure.com/acme/payments/_git/payments-api?path=/src/payments/client.py&version=GC${SHA}&line=46&lineEnd=49&lineStartColumn=1&lineEndColumn=1&lineStyle=plain&_a=contents`,
      `https://dev.azure.com/acme/payments/_git/payments-api/commit/${SHA}`,
    ],
  ];
  for (const [url, line, range, commit] of cases) {
    assert.equal(address(lineLink(url)), line, url);
    assert.equal(address(lineLink(url, { endLine: 48 })), range, url);
    assert.equal(address({ remote: { name: "origin", url }, commit: SHA }), commit, url);
  }
  // A file name links to the whole file; a path's spaces and # are escaped.
  assert.equal(
    address({ remote: { name: "o", url: "git@github.com:a/b.git" }, commit: SHA, path: "docs/My notes #2.md" }),
    `https://github.com/a/b/blob/${SHA}/docs/My%20notes%20%232.md`,
  );
});

test("a host Spelunk can't place needs spelunk.permalinks.hosts; names with github or gitlab in them are guessed", () => {
  // @covers setting:permalinks.hosts
  const company = lineLink("git@git.acme.corp:payments/api.git");
  assert.deepEqual(permalink(company, {}), { problem: "unknownHost", host: "git.acme.corp" });
  assert.equal(
    address(company, { "git.acme.corp": "gitlab" }),
    `https://git.acme.corp/payments/api/-/blob/${SHA}/src/payments/client.py#L46`,
  );
  assert.ok(
    address(lineLink("https://github.acme.corp/payments/api.git")).startsWith(
      "https://github.acme.corp/payments/api/blob/",
    ),
  );
  // A value that isn't a kind of host is ignored.
  assert.deepEqual(permalink(company, { "git.acme.corp": "sourcehut" }), {
    problem: "unknownHost",
    host: "git.acme.corp",
  });
});

/** A Git repo's remotes at `urls`, the first named origin, linking to `preferred`. */
const remotes = (preferred?: string, ...urls: string[]) => ({
  git: true,
  remotes: urls.map((url, index) => ({ name: index === 0 ? "origin" : `r${index}`, url })),
  ...(preferred ? { preferred } : {}),
});

test("which kind of host a repo's menu names", () => {
  assert.equal(repoHost({ git: false, remotes: [] }, {}), "none");
  assert.equal(repoHost(remotes(), {}), "none");
  assert.equal(repoHost(remotes("origin", "git@gitlab.com:a/b.git"), {}), "gitlab");
  assert.equal(repoHost(remotes("origin", "git@git.acme.corp:a/b.git"), {}), "other");
  assert.equal(repoHost(remotes("origin", "git@git.acme.corp:a/b.git"), { "git.acme.corp": "gitea" }), "gitea");
  assert.equal(repoHost(remotes(undefined, "git@github.com:a/b.git", "git@github.com:c/b.git"), {}), "pick");
});

test("a result without a link says why, in plain words", () => {
  const github = { name: "origin", url: "git@github.com:acme/payments-api.git" };
  const why = (link: Link, where = "line 52") => {
    const made = permalink(link, {});
    assert.ok("problem" in made);
    return problemMessage(made, "payments-api", where);
  };
  assert.equal(
    why({ remote: github, problem: "lineChanged" }),
    "Line 52 has changes GitHub doesn't have yet. Commit and push them, then copy the link again.",
  );
  assert.equal(
    why({ remote: github, problem: "notPushed" }, "this commit"),
    "GitHub doesn't have this commit yet. Push it, then copy the link again.",
  );
  assert.equal(why({ problem: "noRemote" }), "payments-api has no Git remote, so there's nowhere to link to.");
  assert.equal(why({ problem: "notGit" }), "payments-api isn't a Git repository, so there's nowhere to link to.");
  assert.match(why({ remote: github, problem: "notOnRemote" }), /^GitHub doesn't have this file yet/u);
  assert.deepEqual(permalink({ problem: "pickRemote", remotes: [github] }, {}), {
    problem: "pickRemote",
    remotes: [github],
  });
});

test("the panel learns which kind of host each folder's links go to; a folder the daemon can't answer for is left out", async () => {
  // @covers msg:remotes.state rpc:repo/remotes
  const backend = {
    request: (_method: string, { repoId }: { repoId: string }) => {
      if (repoId === "r1") return Promise.resolve(remotes("origin", "git@git.acme.corp:a/b.git"));
      if (repoId === "r2") return Promise.resolve({ git: true, remotes: [] });
      return Promise.reject(new Error("repo no longer open"));
    },
  } as never;
  const roots = ["r1", "r2", "r3"].map((id) => ({ id, path: `/work/${id}`, name: id }));
  assert.deepEqual(await remotesState(backend, roots, { "git.acme.corp": "gitlab" }), {
    repos: [
      { repoId: "r1", host: "gitlab" },
      { repoId: "r2", host: "none" },
    ],
  });
});
