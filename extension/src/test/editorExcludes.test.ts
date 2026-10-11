// VS Code's files.exclude and search.exclude, read for each workspace folder.
// @covers setting:search.useExcludeSettings
import assert from "node:assert/strict";
import { test } from "node:test";
import { editorExcludes, type FolderExcludes } from "../editorExcludes";
import { makeRoot } from "../roots";

const web = makeRoot("/work/web");
const api = makeRoot("/work/api");

/** Each folder's settings, by folder path and section. */
function settings(values: Record<string, Partial<Record<"files" | "search", unknown>>>): FolderExcludes {
  return (folder, section) => values[folder]?.[section];
}

test("each folder gets its own files.exclude and search.exclude patterns", () => {
  const read = settings({
    "/work/web": {
      files: { "**/.git": true, "**/*.js": { when: "$(basename).ts" } },
      search: { dist: true, "**/node_modules": true },
    },
    "/work/api": { search: { "docs/generated": true } },
  });
  assert.deepEqual(editorExcludes([web, api], read), [
    {
      rootId: web.id,
      patterns: [
        { glob: "**/.git" },
        { glob: "**/*.js", when: "$(basename).ts" },
        { glob: "dist" },
        { glob: "**/node_modules" },
      ],
    },
    { rootId: api.id, patterns: [{ glob: "docs/generated" }] },
  ]);
});

test("a pattern set to false is off, and search.exclude can turn off a files.exclude pattern", () => {
  const read = settings({
    "/work/web": { files: { "**/out": true, "**/.git": true }, search: { "**/out": false, build: false } },
  });
  assert.deepEqual(editorExcludes([web], read), [{ rootId: web.id, patterns: [{ glob: "**/.git" }] }]);
});

test("folders with no patterns, and values that aren't settings, are left out", () => {
  const read = settings({
    "/work/web": { files: ["dist"], search: "dist" },
    "/work/api": { search: { "": true, a: 1 } },
  });
  assert.deepEqual(editorExcludes([web, api], read), []);
});
