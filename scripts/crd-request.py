#!/usr/bin/env python3
"""The crd-request workflow's helper (.github/workflows/crd-request.yml): reads a "Plugin
request from a CRD" issue (.github/ISSUE_TEMPLATE/crd-request.yml) and writes what the workflow
posts back. Issue bodies come from anyone on the internet: every value leaves this script
validated, on one line, and the shell never sees the raw body.

Usage (run with python3 -I):
  crd-request.py parse                  ISSUE_BODY, ISSUE_NUMBER from the environment; prints
                                        crd_url=, crds=, operator=, docs=, id=, branch= lines
                                        for $GITHUB_OUTPUT, or one reason on stderr and exit 1
                                        (crd_url is empty when the request names the CRDs instead)
  crd-request.py search                 CRDS (names, space-separated), GH_TOKEN: searches public
                                        GitHub for the first CRD's manifest; prints crd_url=,
                                        website=, repo= for $GITHUB_OUTPUT, or a reason and exit 1
  crd-request.py check-url URL          exit 0 when URL is on the allowlist (the address curl
                                        ended on after redirects), else a reason and exit 1
  crd-request.py refused REASON_FILE    the comment for a request refused before any fetch
  crd-request.py failed LOG_FILE        the comment for a CRD the generator or the lint rejects
  crd-request.py is-covered LOG_FILE    exit 0 when the generator refused a CRD the app already reads
  crd-request.py covered LOG_FILE       the comment for a CRD a built-in plugin already reads
  crd-request.py pr-body LINT_FILE      the draft pull request's body (CRD_URL, OPERATOR,
                                        ISSUE_NUMBER, FALLBACK=1 when GITHUB_TOKEN opens it)
  crd-request.py pushed LINT_FILE       the issue comment when no pull request could be opened
                                        (COMPARE_URL, BRANCH)
"""

import json
import os
import re
import sys
from urllib.error import HTTPError, URLError
from urllib.parse import quote, urlencode, urlsplit, urlunsplit
from urllib.request import Request, urlopen

# Hosts a CRD manifest is fetched from: public forges' raw files, nothing else.
HOSTS = {"raw.githubusercontent.com", "github.com", "gitlab.com", "codeberg.org"}
EXTENSIONS = (".yaml", ".yml", ".json")
MAX_BODY = 65536  # GitHub caps an issue body at 65,536 characters
MAX_URL = 2048
# RFC 3986 characters (no space, backtick, quote or angle bracket): anything else is refused.
# "@" is allowed in a path; in the authority (a user name) check_url refuses it.
URL_CHARS = re.compile(r"[A-Za-z0-9._~:/?#\[\]@!$&'()*+,;=%-]+")
OPERATOR = re.compile(r"[A-Za-z0-9 ._-]{1,60}")
# A CRD name as the API serves it: "<plural>.<group>", lower-case DNS labels.
CRD_NAME = re.compile(r"[a-z0-9-]+(?:\.[a-z0-9-]+)+")
MAX_CRDS = 20
# Public GitHub code search, for a request that names the CRDs but has no link.
API = "https://api.github.com"
REPO_NAME = re.compile(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+")
BRANCH_NAME = re.compile(r"[A-Za-z0-9._/-]+")
MAX_QUERIES = 6  # code search allows 10 a minute
MAX_REPOS = 5  # candidates looked up for their stars and home page
# A repository named after the project wins over any number of stars: mirrors and chart
# collections have more files, not the project. The name comes from the group's labels ("kueue"
# in kubernetes-sigs/kueue) or from an operator catalog, whose layout
# operators/<project>/<version>/manifests/ names it (cloudnative-pg, whose CRDs live in
# cloudnative-pg/artifacts, a repository no label of postgresql.cnpg.io names).
NAME_MATCH = 1_000_000
CATALOG_PATH = re.compile(r"(?:^|.*/)operators/([a-z0-9-]+)/[^/]+/manifests/[^/]+")
VERSION_SEGMENT = re.compile(r"v?\d+(?:\.\d+)+")
# A CRD link's path: no "[]{}" (curl globs) and nothing Markdown reads as a link or an image.
CRD_PATH = re.compile(r"[A-Za-z0-9._~/%+-]+")
NO_RESPONSE = "_No response_"
MAX_LOG_LINES = 60
# plugin-from-crd's refusal of a CRD the app already reads (go/cmd/probe/cmd_plugin_crd.go).
COVERED = re.compile(r"\S+: already covered by ((?:built-in plugin|native integration) [a-z0-9-]+) \(-force writes a draft anyway\)")


class Refused(Exception):
    """A request the workflow does not act on; its message is posted on the issue."""


def sections(body: str) -> dict[str, str]:
    """The issue form's fields: each "### Label" heading and the text under it."""
    out, label, lines = {}, None, []
    for line in body.replace("\r\n", "\n").split("\n"):
        if line.startswith("### "):
            if label is not None:
                out[label] = "\n".join(lines).strip()
            label, lines = line[4:].strip(), []
        elif label is not None:
            lines.append(line)
    if label is not None:
        out[label] = "\n".join(lines).strip()
    return {k: ("" if v == NO_RESPONSE else v) for k, v in out.items()}


def raw_url(url: str) -> str:
    """The raw file a forge page link points at; a raw link as it is."""
    parts = urlsplit(url)
    host, path = parts.hostname, parts.path
    if host == "github.com":
        m = re.fullmatch(r"/([^/]+)/([^/]+)/(?:blob|raw)/(.+)", path)
        if not m:
            raise Refused("a github.com link must point at a file (…/blob/<branch>/<path>)")
        host, path = "raw.githubusercontent.com", f"/{m[1]}/{m[2]}/{m[3]}"
    elif host == "gitlab.com":
        path = path.replace("/-/blob/", "/-/raw/", 1)
        if "/-/raw/" not in path:
            raise Refused("a gitlab.com link must point at a file (…/-/blob/<branch>/<path>)")
    elif host == "codeberg.org":
        path = re.sub(r"^(/[^/]+/[^/]+)/src/", r"\1/raw/", path)
        if not re.match(r"^/[^/]+/[^/]+/raw/", path):
            raise Refused("a codeberg.org link must point at a file (…/src/branch/<branch>/<path>)")
    return urlunsplit(("https", host, path, "", ""))


def check_url(url: str, field: str = "the CRD manifest URL", hosts=HOSTS) -> None:
    """Refuses a URL that is not a plain https link on hosts (any host when hosts is None)."""
    if not url:
        raise Refused(f"{field} is empty")
    if len(url) > MAX_URL or not URL_CHARS.fullmatch(url):
        raise Refused(f"{field} is not a plain link (at most {MAX_URL} characters, no spaces)")
    try:
        parts = urlsplit(url)
        port = parts.port
    except ValueError:
        raise Refused(f"{field} is not a valid link") from None
    if parts.scheme != "https":
        raise Refused(f"{field} must start with https://")
    if "@" in parts.netloc or port is not None:
        raise Refused(f"{field} must not carry a user name, a password or a port")
    if hosts is not None and parts.hostname not in hosts:
        raise Refused(f"{field} must be on {', '.join(sorted(hosts))}")
    if hosts is not None and parts.query:
        raise Refused(f"{field} must not carry a query (a token in a link is private)")


def crd_url(value: str) -> str:
    check_url(value)
    url = raw_url(value)
    if not CRD_PATH.fullmatch(urlsplit(url).path):
        raise Refused("the CRD manifest URL's path must be plain letters, digits and . _ ~ / % + -")
    if not urlsplit(url).path.lower().endswith(EXTENSIONS):
        raise Refused("the CRD manifest URL must end in .yaml, .yml or .json")
    return url


def crd_names(text: str) -> list[str]:
    """The CRD names of the request, one per line; code fences and backticks are ignored."""
    names: list[str] = []
    for line in text.splitlines():
        line = line.strip()
        if not line or line.startswith("```"):
            continue
        line = line.strip("`")
        if len(line) > 253 or not CRD_NAME.fullmatch(line):
            raise Refused("a CRD name must be <plural>.<group> in lower-case letters, digits, dots and dashes")
        if line not in names:
            names.append(line)
    if len(names) > MAX_CRDS:
        raise Refused(f"at most {MAX_CRDS} CRD names")
    return names


def plugin_id(operator: str) -> str:
    """The plugin id the generator would make from a group, made from the operator."""
    label = operator.lower()
    for tld in (".io", ".com", ".dev", ".org"):
        label = label.removesuffix(tld)
    label = label.split(".", 1)[0]
    return re.sub(r"[^a-z0-9-]+", "-", label).strip("-")[:40].strip("-")


def parse(body: str, number: str) -> dict[str, str]:
    if len(body) > MAX_BODY:
        raise Refused("the issue body is longer than GitHub allows")
    if not number.isdigit():
        raise Refused("no issue number")
    fields = sections(body)
    url = fields.get("CRD manifest URL", "")
    url = crd_url(url) if url else ""
    crds = crd_names(fields.get("CRD names", ""))
    if not url and not crds:
        raise Refused("the CRD manifest URL is empty and no CRD name is given")
    operator = fields.get("Operator", "")
    if not OPERATOR.fullmatch(operator):
        raise Refused("the operator must be 1 to 60 letters, digits, spaces, dots, dashes or underscores")
    pid = plugin_id(operator)
    if not pid:
        raise Refused("no plugin id can be made from the operator")
    docs = fields.get("Documentation link", "")
    if docs:
        check_url(docs, "the documentation link", hosts=None)
    return {
        "crd_url": url, "crds": " ".join(crds), "operator": operator, "docs": docs, "id": pid,
        "branch": f"crd-request/{number}-{pid}",
    }


def github(path: str, params: dict[str, str], token: str) -> dict:
    """One GitHub REST call; a failure is a Refused with its status, never a traceback."""
    headers = {"Accept": "application/vnd.github+json", "X-GitHub-Api-Version": "2022-11-28"}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    url = f"{API}{path}" + (f"?{urlencode(params)}" if params else "")
    try:
        with urlopen(Request(url, headers=headers), timeout=20) as r:
            return json.load(r)
    except HTTPError as e:
        raise Refused(f"GitHub answered {e.code} to {path}") from None
    except (URLError, TimeoutError, ValueError) as e:
        raise Refused(f"GitHub could not be reached for {path}: {e}") from None


def search_queries(crd: str) -> list[str]:
    """Where a project publishes the CRD: kubebuilder's <group>_<plural>.yaml, then its text."""
    plural, group = crd.split(".", 1)
    return [f"filename:{group}_{plural}.yaml", f"filename:{group}_{plural}.yml", f'"name: {crd}" extension:yaml']


def group_labels(crd: str) -> list[str]:
    """The labels of the CRD's group that can name its project: "postgresql", "cnpg" for
    clusters.postgresql.cnpg.io (the top-level domain and short labels left out)."""
    return [label for label in crd.split(".")[1:-1] if len(label) >= 3]


def project_hints(crd: str, found: dict[str, list[str]]) -> list[str]:
    """What the project may be called: the group's labels and the names operator catalogs file
    the CRD under."""
    hints = group_labels(crd)
    for paths in found.values():
        for path in paths:
            m = CATALOG_PATH.fullmatch(path)
            if m and len(m[1]) >= 3 and m[1] not in hints:
                hints.append(m[1])
    return hints


def natural(text: str) -> list:
    return [int(t) if t.isdigit() else t for t in re.split(r"(\d+)", text)]


def best_path(paths: list[str]) -> str:
    """The source manifest when there is one (no version in its path; a crd directory first),
    else the newest bundle."""
    plain = [p for p in paths if not any(VERSION_SEGMENT.fullmatch(seg) for seg in p.split("/"))]
    return min(plain, key=lambda p: ("crd" not in p.lower(), len(p))) if plain else max(paths, key=natural)


def manifest_candidates(items: list[dict]) -> dict[str, list[str]]:
    """The manifests found, by repository, in search order: plain files only, never a Helm
    template (its directives are not YAML) nor a fork."""
    out: dict[str, list[str]] = {}
    for item in items:
        repo, path = item.get("repository", {}), item.get("path", "")
        name = repo.get("full_name", "")
        if repo.get("fork") or not REPO_NAME.fullmatch(name) or not CRD_PATH.fullmatch(path):
            continue
        if "/templates/" in f"/{path}" or not path.lower().endswith(EXTENSIONS):
            continue
        out.setdefault(name, []).append(path)
    return out


def search(crds: list[str], token: str, api=github) -> dict[str, str]:
    """The public manifest of the first CRD that is found: the repository named after the
    group's project, else the most starred one; its home page as the website."""
    queries = 0
    for crd in crds:
        for query in search_queries(crd):
            if queries == MAX_QUERIES:
                break
            queries += 1
            found = manifest_candidates(api("/search/code", {"q": query, "per_page": "100"}, token).get("items", []))
            if found:
                return pick_manifest(crd, found, token, api)
    raise Refused(f"no public manifest was found on GitHub for {', '.join(crds[:2])}; give its link")


def pick_manifest(crd: str, found: dict[str, list[str]], token: str, api) -> dict[str, str]:
    hints = project_hints(crd, found)
    # The repositories named after the project first, so they are among those looked up.
    ranked = sorted(found, key=lambda name: not any(h in name.lower() for h in hints))
    best, best_score, best_repo = None, -1, {}
    for name in ranked[:MAX_REPOS]:
        repo = api(f"/repos/{name}", {}, token)
        if repo.get("archived") or repo.get("fork"):
            continue
        score = int(repo.get("stargazers_count") or 0)
        if any(h in name.lower() for h in hints):
            score += NAME_MATCH
        if score > best_score:
            best, best_score, best_repo = (name, best_path(found[name])), score, repo
    if best is None:
        raise Refused(f"the manifests found for {crd} are all in archived repositories; give its link")
    name, path = best
    branch = str(best_repo.get("default_branch") or "")
    if not BRANCH_NAME.fullmatch(branch):
        raise Refused(f"{name} has no usable default branch")
    homepage = str(best_repo.get("homepage") or "")
    try:
        check_url(homepage, "the home page", hosts=None)
    except Refused:
        homepage = f"https://github.com/{name}"
    return {"crd_url": crd_url(f"https://raw.githubusercontent.com/{name}/{quote(branch)}/{quote(path)}"), "website": homepage, "repo": name}


def fenced(text: str) -> str:
    """text in a code block no line of it can close, cut to MAX_LOG_LINES lines."""
    lines = text.strip().splitlines()
    if len(lines) > MAX_LOG_LINES:
        lines = lines[:MAX_LOG_LINES] + [f"… {len(lines) - MAX_LOG_LINES} more lines in the run log"]
    longest = max((len(m) for m in re.findall(r"`+", text)), default=0)
    fence = "`" * max(3, longest + 1)
    return f"{fence}text\n" + "\n".join(lines) + f"\n{fence}"


def read(path: str) -> str:
    with open(path, encoding="utf-8", errors="replace") as f:
        return f.read()


def covered_by(log: str) -> str:
    """What already reads the CRD, from the generator's "already covered by …" error."""
    m = COVERED.fullmatch(last_line(log))
    return m[1] if m else ""


def last_line(log: str) -> str:
    lines = [line for line in log.strip().splitlines() if line.strip()]
    return lines[-1] if lines else ""


PROMOTION = """\
To promote the draft to a built-in plugin:
- [ ] Review the health rules and their reasons, and the findings.
- [ ] Check the samples (invented names only) and the cards in `just probe plugin-render go/plugin/drafts/<id>.yaml -demo`.
- [ ] Translate every label for fr, de, es, it and uk (`i18n`).
- [ ] `git mv go/plugin/drafts/<id>.yaml go/plugin/builtin/`, then `just plugin-check` and `go test ./plugin/...`."""


def pr_body(lint: str, env: dict[str, str]) -> str:
    parts = [
        f"Closes #{env['ISSUE_NUMBER']}",
        f"A plugin draft for **{env['OPERATOR']}**, generated by `probe plugin-from-crd` from `{env['CRD_URL']}`. "
        "Nothing under `go/plugin/drafts/` is embedded in the app.",
    ]
    if env.get("FOUND_REPO"):
        parts.append(
            f"The request gave no link: the manifest was found by searching GitHub for the CRD's name, in "
            f"**{env['FOUND_REPO']}**, and its home page is the draft's website. Check that this is the upstream "
            "project, not a mirror or a chart collection, before promoting."
        )
    parts += [
        "Lint against the CRD:\n\n" + fenced(lint or "no output"),
        PROMOTION.replace("<id>", env.get("ID") or "<id>"),
    ]
    if env.get("FALLBACK") == "1":
        parts.append(
            "Opened with GITHUB_TOKEN: checks do not start on it. Close and reopen this pull request to run CI, "
            "or configure `CRD_BOT_CLIENT_ID` / `CRD_BOT_PRIVATE_KEY` (CONTRIBUTING.md)."
        )
    return "\n\n".join(parts) + "\n"


def main(argv: list[str]) -> int:
    if len(argv) < 2:
        print(__doc__, file=sys.stderr)
        return 2
    cmd, args = argv[1], argv[2:]
    try:
        if cmd == "parse":
            for key, value in parse(os.environ.get("ISSUE_BODY", ""), os.environ.get("ISSUE_NUMBER", "")).items():
                print(f"{key}={value}")
        elif cmd == "check-url":
            check_url(args[0])
        elif cmd == "search":
            for key, value in search(os.environ.get("CRDS", "").split(), os.environ.get("GH_TOKEN", "")).items():
                print(f"{key}={value}")
        elif cmd == "refused":
            print(f"This request was not acted on: {read(args[0]).strip()}. Edit the issue, then a maintainer "
                  "removes and adds the `crd-approved` label to run it again.")
        elif cmd == "failed":
            print("The draft could not be generated or does not pass the lint:\n\n" + fenced(read(args[0])))
        elif cmd == "is-covered":
            return 0 if covered_by(read(args[0])) else 1
        elif cmd == "covered":
            print(f"This CRD is already covered by {covered_by(read(args[0]))}: no draft is needed. "
                  "If something is missing there, say what in an integration request.")
        elif cmd == "pr-body":
            print(pr_body(read(args[0]), dict(os.environ)), end="")
        elif cmd == "pushed":
            print(f"The draft is on the branch `{os.environ['BRANCH']}`; a maintainer opens the pull request: "
                  f"{os.environ['COMPARE_URL']}\n\nLint against the CRD:\n\n{fenced(read(args[0]) or 'no output')}")
        else:
            print(__doc__, file=sys.stderr)
            return 2
    except Refused as e:
        print(e, file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
