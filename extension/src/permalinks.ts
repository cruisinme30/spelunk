// Permalinks as web addresses: which kind of Git host a remote is, and each
// host's link format for a file at a commit (at a line or lines) and for a
// commit. The daemon's link/resolve says what a link names; this turns it
// into the address. No vscode import, so it's tested in plain Node.
import type { Backend } from "./controller";
import type { Link, LinkProblem, Remote, RemoteHost, RemotesStateMessage, RepoRemotes, Root } from "./protocol.gen";

/** The Git hosts whose link formats Spelunk knows. */
export const HOST_KINDS = ["github", "gitlab", "bitbucket", "gitea", "azure"] as const;
/** One of HOST_KINDS. */
export type HostKind = (typeof HOST_KINDS)[number];

/** How each host is named in the menu and messages. */
export const HOST_LABEL: Record<HostKind, string> = {
  github: "GitHub",
  gitlab: "GitLab",
  bitbucket: "Bitbucket",
  gitea: "Gitea",
  azure: "Azure DevOps",
};

/** spelunk.permalinks.hosts: a host name, as in the remote's URL, to the kind of host it is. */
export type HostSettings = Readonly<Record<string, string>>;

/** A remote as a website: its kind, host name and the repository's web address. */
interface WebRemote {
  /** Undefined for a host Spelunk can't place without spelunk.permalinks.hosts. */
  kind: HostKind | undefined;
  host: string;
  /** https://host/owner/repo, never with the URL's user, password or port. */
  web: string;
  /** The repository's path parts, which Azure DevOps addresses differently. */
  parts: string[];
}

const isHostKind = (value: string | undefined): value is HostKind => HOST_KINDS.includes(value as HostKind);

/** The kind of host a host name is: the setting first, then the well-known hosts and names. */
function kindOf(host: string, hosts: HostSettings): HostKind | undefined {
  const set = hosts[host];
  if (isHostKind(set)) return set;
  if (host === "github.com" || /(^|\.)github\./u.test(host)) return "github";
  if (host === "gitlab.com" || /(^|\.)gitlab\./u.test(host)) return "gitlab";
  if (host === "bitbucket.org") return "bitbucket";
  if (host === "codeberg.org" || /(^|\.)(gitea|forgejo)\./u.test(host)) return "gitea";
  if (host === "dev.azure.com" || host === "ssh.dev.azure.com" || host.endsWith(".visualstudio.com")) return "azure";
  return undefined;
}

/**
 * Reads a remote URL as a website: git@github.com:acme/api.git,
 * ssh://git@ssh.github.com:443/acme/api.git and
 * https://user:token@github.com/acme/api.git are all https://github.com/acme/api.
 */
export function webRemote(url: string, hosts: HostSettings = {}): WebRemote | undefined {
  let host: string;
  let path: string;
  if (/^[a-z][a-z0-9+.-]*:\/\//iu.test(url)) {
    let parsed: URL;
    try {
      parsed = new URL(url);
    } catch {
      return undefined;
    }
    host = parsed.hostname;
    path = decodeURIComponent(parsed.pathname);
  } else {
    // scp-like: [user@]host:path
    const scp = /^(?:[^@/]+@)?([^:/]+):(.+)$/u.exec(url);
    if (!scp) return undefined;
    host = scp[1] ?? "";
    path = scp[2] ?? "";
  }
  path = path
    .replace(/^\/+/u, "")
    .replace(/\.git\/?$/u, "")
    .replace(/\/+$/u, "");
  if (!host || !path) return undefined;
  if (host === "ssh.github.com") host = "github.com"; // GitHub's SSH over port 443
  const kind = kindOf(host, hosts);
  let parts = path.split("/");
  if (kind === "azure") {
    // git@ssh.dev.azure.com:v3/org/project/repo and https://dev.azure.com/org/project/_git/repo
    parts = parts.filter((part) => part !== "v3" && part !== "_git");
    if (host.endsWith("dev.azure.com")) host = "dev.azure.com";
  }
  return { kind, host, web: `https://${host}/${path}`, parts };
}

/** Which kind of host a repo's links go to, for the result menu's items. */
export function repoHost(remotes: RepoRemotes, hosts: HostSettings): RemoteHost {
  if (!remotes.git || remotes.remotes.length === 0) return "none";
  const linked = remotes.remotes.find((remote) => remote.name === remotes.preferred);
  if (!linked) return "pick";
  return webRemote(linked.url, hosts)?.kind ?? "other";
}

/**
 * Which kind of Git host each workspace folder's links go to, for the
 * panel's remotes.state. A folder the daemon can't answer for (it's down,
 * or the folder just closed) is left out, and its link items stay generic.
 */
export async function remotesState(
  backend: Pick<Backend, "request">,
  roots: Root[],
  hosts: HostSettings,
): Promise<RemotesStateMessage> {
  const repos = await Promise.all(
    roots.map(async (root) => {
      try {
        const remotes = await backend.request("repo/remotes", { repoId: root.id });
        return { repoId: root.id, host: repoHost(remotes, hosts) };
      } catch {
        return; // left out
      }
    }),
  );
  return { repos: repos.filter((repo) => repo !== undefined) };
}

/** A path's segments, each URL-encoded: spaces, # and ? included. */
const encodePath = (path: string) =>
  path
    .split("/")
    .map((part) => encodeURIComponent(part))
    .join("/");

/** The hosts whose addresses are the repository's web address, then a path, the commit and the file. */
type PlainHost = Exclude<HostKind, "azure">;

/** What comes between the repository's address and the commit, for a file and for a commit. */
const FILE_PART: Record<PlainHost, string> = {
  github: "blob",
  gitlab: "-/blob",
  bitbucket: "src",
  gitea: "src/commit",
};
const COMMIT_PART: Record<PlainHost, string> = {
  github: "commit",
  gitlab: "-/commit",
  bitbucket: "commits",
  gitea: "commit",
};

/** How each host marks a line, or lines line–end, after a file's address. */
const LINE_ANCHOR: Record<PlainHost, (line: number, end: number | undefined) => string> = {
  github: (line, end) => `#L${line}${end ? `-L${end}` : ""}`,
  gitlab: (line, end) => `#L${line}${end ? `-${end}` : ""}`,
  bitbucket: (line, end) => `#lines-${line}${end ? `:${end}` : ""}`,
  gitea: (line, end) => `#L${line}${end ? `-L${end}` : ""}`,
};

/** A remote whose kind of host is known. */
type KnownRemote = WebRemote & { kind: HostKind };

/** Azure DevOps' org, project and repository, each URL-encoded. */
function azureParts(remote: KnownRemote): string {
  const [org, project, repo] = remote.parts.map((part) => encodeURIComponent(part));
  return `https://${remote.host}/${org}/${project}/_git/${repo}`;
}

/** The address of a file at a commit, at its line or lines when the link has them. */
function fileAddress(remote: KnownRemote, link: Link & { commit: string; path: string }): string {
  const { commit, line } = link;
  const end = line !== undefined && link.endLine !== undefined && link.endLine > line ? link.endLine : undefined;
  if (remote.kind === "azure") {
    const lines = line
      ? `&line=${line}&lineEnd=${(end ?? line) + 1}&lineStartColumn=1&lineEndColumn=1&lineStyle=plain`
      : "";
    return `${azureParts(remote)}?path=/${encodePath(link.path)}&version=GC${commit}${lines}&_a=contents`;
  }
  const anchor = line ? LINE_ANCHOR[remote.kind](line, end) : "";
  return `${remote.web}/${FILE_PART[remote.kind]}/${commit}/${encodePath(link.path)}${anchor}`;
}

/** The address of a commit. */
function commitAddress(remote: KnownRemote, commit: string): string {
  if (remote.kind === "azure") return `${azureParts(remote)}/commit/${commit}`;
  return `${remote.web}/${COMMIT_PART[remote.kind]}/${commit}`;
}

/** A permalink, or why there's none; `host` names the remote's kind of host when it has one. */
export type Permalink =
  | { url: string; host: string; kind: HostKind }
  | { problem: LinkProblem | "unknownHost"; host?: string; remotes?: Remote[] };

/** Turns what link/resolve says into the address on the remote's host. */
export function permalink(link: Link, hosts: HostSettings): Permalink {
  const remote = link.remote && webRemote(link.remote.url, hosts);
  const label = remote?.kind ? HOST_LABEL[remote.kind] : remote?.host;
  if (link.problem !== undefined) {
    return {
      problem: link.problem,
      ...(label ? { host: label } : {}),
      ...(link.remotes ? { remotes: link.remotes } : {}),
    };
  }
  if (!remote || link.commit === undefined) return { problem: "noRemote" };
  if (!remote.kind) return { problem: "unknownHost", host: remote.host };
  const known = { ...remote, kind: remote.kind };
  const { commit, path } = link;
  const url = path === undefined ? commitAddress(known, commit) : fileAddress(known, { ...link, commit, path });
  return { url, host: HOST_LABEL[remote.kind], kind: remote.kind };
}

/**
 * Why a result has no permalink, in a sentence for a notification.
 * `where` is the result as the sentence names it: "line 52", "this commit", "this file".
 */
export function problemMessage(missing: Exclude<Permalink, { url: string }>, repo: string, where: string): string {
  const host = missing.host ?? "the remote";
  switch (missing.problem) {
    case "notGit": {
      return `${repo} isn't a Git repository, so there's nowhere to link to.`;
    }
    case "noRemote": {
      return `${repo} has no Git remote, so there's nowhere to link to.`;
    }
    case "pickRemote": {
      return `${repo} has several remotes and its branch tracks none of them.`;
    }
    case "notPushed": {
      return `${host} doesn't have ${where} yet. Push it, then copy the link again.`;
    }
    case "lineChanged": {
      return `${where[0]?.toUpperCase() ?? ""}${where.slice(1)} has changes ${host} doesn't have yet. Commit and push them, then copy the link again.`;
    }
    case "notOnRemote": {
      return `${host} doesn't have this file yet. Commit and push it, then copy the link again.`;
    }
    case "unknownHost": {
      return `Spelunk doesn't know which kind of Git host ${host} is. Set it in spelunk.permalinks.hosts, for example "${host}": "gitlab".`;
    }
  }
}
