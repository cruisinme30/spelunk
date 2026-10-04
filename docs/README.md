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

User docs (tutorial, how-to, reference) are written as each feature lands. The query-language reference comes with
M1, settings with M5.

## For contributors

- [ARCHITECTURE.md](../ARCHITECTURE.md): the code map and the rules that always hold.
- [CONTRIBUTING.md](../CONTRIBUTING.md): setup, commit and code conventions, tests.
- [dev/implementation-plan.md](dev/implementation-plan.md): scope, the five contracts, indexing, budgets, milestones,
  risks.
- [dev/test-plan.md](dev/test-plan.md): test layers, the E2E harness, scenarios E01–E18 and X01–X12, CI gates.
- [dev/mocks.md](dev/mocks.md): index of the 18 UI mocks and their milestones.
- [dev/progress.md](dev/progress.md): milestone sign-offs with evidence.
