# Discoverable MCP setup with executable configuration

**Issue.** Agent setup required knowing the stdio invocation. The bare MCP command waited for a client instead of
explaining installation.

**Example.** A client installed under `bin with spaces` must launch the generated command when its
repository also contains spaces and its current directory is elsewhere. Adding shell quotes inside
JSON argument strings would change the actual path passed to the program.

**Root cause.** Server operation and setup discovery shared one entry point without help or configuration output.
The default search folder depended on the client's current directory.

**Change.** Add `spelunk mcp --help` and `spelunk mcp config [--in DIR]`. Configuration contains a command and an
argument array, preserving paths without shell quoting. `spelunk mcp --in DIR` selects an explicit default folder;
the bare command retains its existing stdio behavior and current-directory default. Invalid arguments and missing
explicit folders fail before serving. Help and configuration work without an existing index.

**Why it works.** Users can inspect setup without entering a server session, then give the client the same executable
and argument boundaries validated locally. Explicit folder selection is independent of the client's
launch directory. Individual tool calls can still choose another absolute folder.

**Validation.** `daemon/internal/cli/mcp_setup_test.go` checks help, invalid options, repeatable JSON, spaces and
missing directories. `extension/src/test/mcpSetup.test.ts` launches the actual installed binary from an unrelated
directory, completes MCP initialization, lists tools, requests syntax and exits cleanly. Moving the configured
folder then makes the same generated invocation fail, proving `--in` is honored. Owner checks passed.

**Limits.** Clients may store configuration in different formats; reuse the command and arguments in their supported
layout. MCP remains read-only and requires an index for search. External client applications are not certified here.

**Status and paths.** Implemented on the working branch; final integration and publication are pending. Main code:
`daemon/internal/cli/mcp_setup.go` and `daemon/internal/cli/cli.go`.
