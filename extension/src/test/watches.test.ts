// What query watches found that hasn't been looked at, and their pop-up text.
import assert from "node:assert/strict";
import { test } from "node:test";
import type { UnseenCommits, WatchMatchedParams } from "../protocol.gen";
import { MAX_UNSEEN_SHAS, UnseenWatches, watchMessage } from "../watches";

/** An announcement for `query` of commits with these shas, subjects "Revert <sha>". */
function matched(query: string, shas: string[], total = shas.length): WatchMatchedParams {
  return {
    query,
    repoId: "r1",
    repoName: "payments-api",
    total,
    commits: shas.map((sha) => ({ sha, subject: `Revert ${sha}`, author: "Jason Kim" })),
  };
}

test("announced commits count per query until the query runs, newest first and without repeats", () => {
  // @covers screen:query-watches
  const saves: UnseenCommits[][] = [];
  const unseen = new UnseenWatches(undefined, (list) => void saves.push(list));
  assert.deepEqual(unseen.record(matched("msg:revert", ["b2", "a1"])), {
    commits: matched("msg:revert", ["b2", "a1"]).commits,
    count: 2,
  });
  assert.equal(unseen.record(matched("msg:revert", ["c3", "b2"])).count, 1, "b2 was announced already");
  assert.equal(unseen.record(matched("msg:revert", ["c3"])).count, 0, "nothing new: nothing recorded");
  assert.deepEqual(unseen.list(), [{ query: "msg:revert", count: 3, shas: ["c3", "b2", "a1"] }]);
  assert.equal(saves.length, 2, "saved after each change, not after an announcement with nothing new");

  assert.deepEqual(unseen.take("msg:revert"), ["c3", "b2", "a1"]);
  assert.deepEqual(unseen.list(), []);
  assert.deepEqual(unseen.take("msg:revert"), [], "taken once");
  assert.deepEqual(saves.at(-1), []);
});

test("commits past the first page still count, and the shas kept are capped", () => {
  // @covers screen:query-watches
  const unseen = new UnseenWatches(undefined);
  assert.equal(unseen.record(matched("msg:revert", ["a1"], 3)).count, 3, "two more matched than were listed");
  assert.equal(
    unseen.record(matched("msg:revert", ["a1"], 3)).count,
    0,
    "the same announcement again, after a restart",
  );
  assert.equal(unseen.list()[0]?.count, 3);
  const many = Array.from({ length: MAX_UNSEEN_SHAS + 5 }, (_, index) => `s${index}`);
  unseen.record(matched("author:jason", many));
  const [, jason] = unseen.list();
  assert.equal(jason?.count, MAX_UNSEEN_SHAS + 5);
  assert.equal(jason.shas.length, MAX_UNSEEN_SHAS);
});

test("the saved state comes back, and malformed or unwatched entries are dropped", () => {
  // @covers screen:query-watches
  const saved: unknown = [
    { query: "msg:revert", count: 2, shas: ["b2", "a1"] },
    { query: "author:jason", count: 1, shas: ["x"] },
    { query: 5, count: 1, shas: [] },
    { query: "msg:fix", count: 0, shas: [] },
    { query: "msg:oops", count: 1, shas: "a1" },
    "nonsense",
  ];
  const saves: UnseenCommits[][] = [];
  const unseen = new UnseenWatches(saved, (list) => void saves.push(list));
  assert.deepEqual(
    unseen.list().map((entry) => entry.query),
    ["msg:revert", "author:jason"],
  );
  unseen.keepOnly(["msg:revert"]);
  assert.deepEqual(unseen.list(), [{ query: "msg:revert", count: 2, shas: ["b2", "a1"] }]);
  assert.equal(saves.length, 1);
  unseen.keepOnly(["msg:revert"]);
  assert.equal(saves.length, 1, "nothing to forget: nothing saved");
  assert.deepEqual(new UnseenWatches({ not: "a list" }).list(), []);
});

test("the pop-up names the pin, the repo and the newest commit", () => {
  // @covers screen:query-watches
  const two = { commits: matched("q", ["b2", "a1"]).commits, count: 2 };
  assert.equal(
    watchMessage("Payments reverts", "payments-api", two),
    "2 new commits match “Payments reverts” in payments-api: “Revert b2” and 1 more.",
  );
  const one = { commits: two.commits.slice(0, 1), count: 1 };
  assert.equal(watchMessage("msg:revert", "web", one), "1 new commit matches “msg:revert” in web: “Revert b2”.");
  assert.equal(
    watchMessage("msg:revert", "web", { commits: [], count: 4 }),
    "4 new commits match “msg:revert” in web.",
  );
});

test("the pop-up shows link syntax in a subject, pin or repo name as text, not as a link", () => {
  // @covers screen:query-watches
  // How VS Code finds the links in a notification's message.
  const notificationLink = /\[([^\]]+)\]\(((?:https?:\/\/|command:|file:)[^)\s]+)(?: (["'])(.+?)(\3))?\)/i;
  const link = "[Show](command:workbench.action.quit)";
  const found = { commits: [{ sha: "a1", subject: `Revert ${link}`, author: "Jason Kim" }], count: 1 };
  const message = watchMessage(`Pin ${link}`, `repo ${link}`, found);
  assert.doesNotMatch(message, notificationLink);
  assert.equal(
    message.replaceAll("\u2060", ""),
    `1 new commit matches “Pin ${link}” in repo ${link}: “Revert ${link}”.`,
    "the text reads as it was",
  );
});
