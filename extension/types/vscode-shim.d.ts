// Offline stand-in for @types/vscode: only the API surface this extension
// uses, with the same signatures. Used by tsconfig.offline.json; normal
// builds use the real @types/vscode and never see this file.
declare module "vscode" {
  export interface Disposable { dispose(): unknown }
  export const Disposable: { from(...d: { dispose(): unknown }[]): Disposable; new (cb: () => void): Disposable };
  export type Event<T> = (listener: (e: T) => unknown, thisArgs?: unknown, disposables?: Disposable[]) => Disposable;
  export class EventEmitter<T> { event: Event<T>; fire(data: T): void; dispose(): void }
  export interface CancellationToken { isCancellationRequested: boolean; onCancellationRequested: Event<unknown> }

  export class Uri {
    static file(path: string): Uri;
    static parse(value: string): Uri;
    static joinPath(base: Uri, ...segments: string[]): Uri;
    readonly scheme: string; readonly path: string; readonly fsPath: string; readonly query: string;
    with(change: { scheme?: string; path?: string; query?: string; fragment?: string }): Uri;
    toString(): string;
  }
  export class Position { constructor(line: number, character: number); readonly line: number; readonly character: number }
  export class Range { constructor(startLine: number, startChar: number, endLine: number, endChar: number); readonly start: Position; readonly end: Position }
  export class Selection extends Range { constructor(anchorLine: number, anchorChar: number, activeLine: number, activeChar: number); readonly anchor: Position; readonly active: Position }
  export enum ViewColumn { Active = -1, Beside = -2, One = 1, Two = 2 }
  export enum StatusBarAlignment { Left = 1, Right = 2 }
  export enum ConfigurationTarget { Global = 1, Workspace = 2, WorkspaceFolder = 3 }
  export enum TextEditorRevealType { Default = 0, InCenter = 1, InCenterIfOutsideViewport = 2, AtTop = 3 }
  export enum FileType { Unknown = 0, File = 1, Directory = 2, SymbolicLink = 64 }

  export interface Memento { get<T>(key: string): T | undefined; get<T>(key: string, defaultValue: T): T; update(key: string, value: unknown): Thenable<void> }
  export interface ExtensionContext {
    subscriptions: { dispose(): unknown }[];
    extensionUri: Uri; extensionPath: string;
    globalState: Memento & { setKeysForSync(keys: readonly string[]): void };
    workspaceState: Memento;
    globalStorageUri: Uri;
    extensionMode: ExtensionMode;
  }
  export enum ExtensionMode { Production = 1, Development = 2, Test = 3 }

  export interface TextDocument { readonly uri: Uri; readonly fileName: string; getText(range?: Range): string; readonly lineCount: number; readonly isDirty: boolean }
  export interface TextEditor { readonly document: TextDocument; selection: Selection; revealRange(range: Range, revealType?: TextEditorRevealType): void; readonly viewColumn: ViewColumn | undefined }
  export interface TextDocumentShowOptions { viewColumn?: ViewColumn; preserveFocus?: boolean; preview?: boolean; selection?: Range }
  export interface TextDocumentContentProvider { onDidChange?: Event<Uri>; provideTextDocumentContent(uri: Uri, token: CancellationToken): string | Thenable<string> }

  export interface OutputChannel { appendLine(value: string): void; show(preserveFocus?: boolean): void; dispose(): void }
  export interface StatusBarItem { text: string; tooltip: string | undefined; command: string | undefined; show(): void; hide(): void; dispose(): void }

  export interface WebviewOptions { enableScripts?: boolean; localResourceRoots?: readonly Uri[] }
  export interface WebviewPanelOptions { retainContextWhenHidden?: boolean; enableFindWidget?: boolean }
  export interface Webview {
    html: string; options: WebviewOptions; readonly cspSource: string;
    asWebviewUri(localResource: Uri): Uri;
    postMessage(message: unknown): Thenable<boolean>;
    onDidReceiveMessage: Event<any>;
  }
  export interface WebviewPanel {
    readonly webview: Webview; readonly visible: boolean; readonly active: boolean; title: string; iconPath?: Uri;
    reveal(viewColumn?: ViewColumn, preserveFocus?: boolean): void;
    onDidDispose: Event<void>;
    onDidChangeViewState: Event<{ webviewPanel: WebviewPanel }>;
    dispose(): void;
  }
  export interface QuickPickItem { label: string; description?: string; detail?: string }

  export namespace window {
    export const activeTextEditor: TextEditor | undefined;
    export function createWebviewPanel(viewType: string, title: string, showOptions: ViewColumn | { viewColumn: ViewColumn; preserveFocus?: boolean }, options?: WebviewPanelOptions & WebviewOptions): WebviewPanel;
    export function createOutputChannel(name: string): OutputChannel;
    export function createStatusBarItem(alignment?: StatusBarAlignment, priority?: number): StatusBarItem;
    export function showTextDocument(uri: Uri, options?: TextDocumentShowOptions): Thenable<TextEditor>;
    export function showTextDocument(document: TextDocument, options?: TextDocumentShowOptions): Thenable<TextEditor>;
    export function showErrorMessage(message: string, ...items: string[]): Thenable<string | undefined>;
    export function showWarningMessage(message: string, ...items: string[]): Thenable<string | undefined>;
    export function showInformationMessage(message: string, ...items: string[]): Thenable<string | undefined>;
    export function showQuickPick<T extends QuickPickItem>(items: readonly T[], options?: { placeHolder?: string }): Thenable<T | undefined>;
  }

  export interface WorkspaceFolder { readonly uri: Uri; readonly name: string; readonly index: number }
  export interface WorkspaceConfiguration { get<T>(section: string): T | undefined; get<T>(section: string, defaultValue: T): T; update(section: string, value: unknown, target?: ConfigurationTarget | boolean): Thenable<void>; inspect<T>(section: string): { key: string; defaultValue?: T; globalValue?: T; workspaceValue?: T } | undefined }
  export interface ConfigurationChangeEvent { affectsConfiguration(section: string): boolean }
  export interface FileSystemWatcher extends Disposable { onDidChange: Event<Uri>; onDidCreate: Event<Uri>; onDidDelete: Event<Uri> }
  export interface WorkspaceFoldersChangeEvent { readonly added: readonly WorkspaceFolder[]; readonly removed: readonly WorkspaceFolder[] }
  export interface FileSystem { readFile(uri: Uri): Thenable<Uint8Array>; stat(uri: Uri): Thenable<{ type: FileType }> }

  export namespace workspace {
    export const workspaceFolders: readonly WorkspaceFolder[] | undefined;
    export const fs: FileSystem;
    export const onDidChangeWorkspaceFolders: Event<WorkspaceFoldersChangeEvent>;
    export const onDidChangeConfiguration: Event<ConfigurationChangeEvent>;
    export const onDidSaveTextDocument: Event<TextDocument>;
    export function getConfiguration(section?: string, scope?: Uri | null): WorkspaceConfiguration;
    export function createFileSystemWatcher(globPattern: string, ignoreCreate?: boolean, ignoreChange?: boolean, ignoreDelete?: boolean): FileSystemWatcher;
    export function openTextDocument(uri: Uri): Thenable<TextDocument>;
    export function registerTextDocumentContentProvider(scheme: string, provider: TextDocumentContentProvider): Disposable;
  }

  export namespace commands {
    export function registerCommand(command: string, callback: (...args: any[]) => any): Disposable;
    export function executeCommand<T = unknown>(command: string, ...rest: any[]): Thenable<T>;
    export function getCommands(filterInternal?: boolean): Thenable<string[]>;
  }

  export namespace env {
    export const appName: string;
    export const remoteName: string | undefined;
    export const uriScheme: string;
    export function openExternal(target: Uri): Thenable<boolean>;
  }

  export namespace languages {
    export function setTextDocumentLanguage(document: TextDocument, languageId: string): Thenable<TextDocument>;
  }
}
