// Entry point of the search panel webview (bundled to dist/webview/main.js).
import { installHoverLabels } from "./hoverLabel";
import { SearchPanel } from "./panel";

const root = document.querySelector<HTMLElement>("#app");
if (root) {
  new SearchPanel(root).start();
  installHoverLabels(root);
}
