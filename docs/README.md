# Documentation

The docs follow [Diátaxis](https://diataxis.fr/): each page has one job. Pick the folder by what the reader is doing.

| Folder | For a reader who wants to… | Examples |
| --- | --- | --- |
| `tutorial/` | learn by doing, start to finish | Your first search session |
| `how-to/` | get a specific task done | Index a huge repo, debug the webview |
| `reference/` | look something up | Query language, settings, protocol |
| `explanation/` | understand why it works this way | How results are ranked and merged |
| [`adr/`](adr/) | know why a decision was made | [Architecture decision records](adr/README.md) |
| [`dev/`](dev/) | build or plan the project itself | Plans and progress, below |

User docs (tutorial, how-to, reference) are written as each feature lands. Until then, the help page inside the
extension (Unified Search: Open Help) is the query-language reference.

## For contributors

- [ARCHITECTURE.md](../ARCHITECTURE.md): the code map and the rules that always hold.
- [CONTRIBUTING.md](../CONTRIBUTING.md): setup, commit and code conventions, tests.
- [dev/implementation-plan.md](dev/implementation-plan.md): the original build plan: scope, interfaces between the
  parts, indexing, performance budgets, the order of work and risks.
- [dev/test-plan.md](dev/test-plan.md): test layers, the end-to-end harness, its scenarios and CI gates.
- [dev/mocks.md](dev/mocks.md): the 18 design mockups, with the screen ids tests use in `@covers screen:…`.
- [dev/progress.md](dev/progress.md): what each stage of the plan delivered, with the tests that prove it.
