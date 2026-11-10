# Why Spelunk

What Spelunk is for, and how it differs from the searches you already have.

## The problem

Answering an everyday question about a codebase often takes several tools. Where is this defined? Who changed it,
and when? What did it look like at the last release? Which of these files does my team own? Each one needs its own
search, in its own window, one repo at a time.

## One box for all of it

Spelunk answers all of those from one search box, across every repo in your VS Code workspace:

| You want | Spelunk query |
| --- | --- |
| Files and code that mention something | `retry_policy` |
| Where a name is defined, or used | `symbol:RetryPolicy`, `ref:RetryPolicy` |
| Code by its shape, over lines | `struct:"backoff(:[call], tries=:[n])"` |
| Commits by someone, or about something | `author:jane timeout`, `message:"fix flaky"` |
| The commit that introduced a line | `type:added retry_policy` |
| The code as it was at a release | `at:v1.0.0 retry_after` |
| A team's files, from CODEOWNERS | `retry owner:payments-core` |

Results come as you type, best match first, and the box helps you write the query: it suggests operators and values,
shows how it read your query, and offers a fix when something is wrong.

## Compared with what you have

| Tool | What it does well | What Spelunk adds |
| --- | --- | --- |
| VS Code's Find in Files | Text and regex in the current files | Commits, definitions and code by shape, from the same box; best match first |
| `git log -S` / `-G` | Commits that added or removed a string, in one repo | Answers as you type, in every repo at once, with each commit's diff in the preview |
| ripgrep | Very fast text search of files | An index kept up to date as you edit, plus history and definitions |
| VS Code's Go to Symbol | Definitions in the open workspace, by name | Definitions by kind (`kind:class`), uses by name (`ref:`), across repos |

## What it isn't

- **Not a server.** Nothing leaves your machine: indexes stay under `index.location`.
- **Not a language server.** `symbol:` and `ref:` match by name, not by meaning, so a different `RetryPolicy` in another
  repo counts too.

---

**Next:** [[Getting Started]]: install it and run your first searches.
