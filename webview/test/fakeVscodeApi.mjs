// The webview's view of VS Code in the test pages (see buildHarness.mjs), bundled ahead of each page's code: VS Code
// gives the body its theme class and the page acquireVsCodeApi.
document.body.classList.add("vscode-dark");
globalThis.__sent = [];
if (!("__savedState" in globalThis)) globalThis.__savedState = undefined;
globalThis.acquireVsCodeApi = () => ({
  postMessage: (message) => {
    // As JSON, which drops undefined fields, so tests compare payloads as the host reads them.
    // eslint-disable-next-line unicorn/prefer-structured-clone
    const copy = JSON.parse(JSON.stringify(message));
    globalThis.__sent.push(copy);
    globalThis.__forwardToHost?.(copy);
  },
  getState: () => globalThis.__savedState,
  setState: (state) => {
    globalThis.__savedState = state;
  },
});
globalThis.__fromHost = (type, payload) =>
  globalThis.dispatchEvent(new MessageEvent("message", { data: { v: 1, type, payload } }));
