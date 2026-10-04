// Entry point of the search panel webview (bundled to dist/webview/main.js).
import { SearchPanel } from "./panel";

const root = document.getElementById("app");
if (root) new SearchPanel(root).start();
