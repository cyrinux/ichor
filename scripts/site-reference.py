#!/usr/bin/env python3
"""Builds the website's reference pages (docs/<name>/index.html) from docs/<name>.md.

The Markdown stays the one source: the Go tests hold docs/plugins.md to the format, the apps'
Settings, Plugins and their Runbooks (i) link to the published pages. The pages are English
only, like the Markdown, and are not committed: CI writes them before publishing
(.github/workflows/pages.yml).

Needs the Markdown package (pip install markdown).

    scripts/site-reference.py    write docs/plugins/index.html and docs/runbooks/index.html
"""
import html
import re
import sys
from pathlib import Path

import markdown

ROOT = Path(__file__).resolve().parent.parent
DOCS = ROOT / "docs"
# docs/<name>.md, published as docs/<name>/index.html.
PAGES = ("plugins", "runbooks")
SITE = "https://cyrinux.github.io/ichor/"
REPO = "https://github.com/cyrinux/ichor/blob/main/docs/"

TEMPLATE = """<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{title} · Ichor</title>
<link rel="canonical" href="{site}{name}/">
<meta name="description" content="{description}">
<link rel="icon" href="../brand/ichor.svg" type="image/svg+xml">
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:wght@400&family=IBM+Plex+Sans:wght@400;600&display=swap" rel="stylesheet">
<style>
  :root {{
    --bg: #E9EEF2;
    --surface: #F6F8FA;
    --ink: #14243B;
    --ink-2: #51627A;
    --line: #C9D3DD;
    --accent: #FF7A45;
    --measure: 80ch;
  }}
  @media (prefers-color-scheme: dark) {{
    :root {{
      --bg: #101823;
      --surface: #16212F;
      --ink: #E4EAF1;
      --ink-2: #9AABC0;
      --line: #2A3A4E;
    }}
  }}
  * {{ box-sizing: border-box; }}
  html {{ -webkit-text-size-adjust: 100%; scroll-padding-top: 1rem; }}
  body {{
    margin: 0;
    background: var(--bg);
    color: var(--ink);
    font: 400 1.0625rem/1.6 "IBM Plex Sans", system-ui, sans-serif;
  }}
  a {{ color: inherit; }}
  a:focus-visible {{ outline: 3px solid var(--accent); outline-offset: 3px; border-radius: 6px; }}
  code {{ font-family: "IBM Plex Mono", ui-monospace, monospace; font-size: .9em; overflow-wrap: anywhere; }}
  pre {{ background: var(--surface); border: 1px solid var(--line); border-radius: 10px; padding: 1rem 1.25rem; overflow-x: auto; line-height: 1.45; }}
  pre code {{ font-size: .85rem; overflow-wrap: normal; }}
  .wrap {{ max-width: 72rem; margin: 0 auto; padding: 0 1.5rem; }}
  .top {{ display: flex; align-items: center; justify-content: space-between; padding: 1.25rem 0; }}
  .brand {{ display: flex; align-items: center; gap: .6rem; font-weight: 600; text-decoration: none; }}
  .brand img {{ width: 32px; height: 32px; }}
  main {{ max-width: var(--measure); padding: 2rem 0 4rem; }}
  h1 {{ font-weight: 600; font-size: clamp(2rem, 4vw, 2.6rem); line-height: 1.1; letter-spacing: -0.02em; margin: 0 0 1rem; }}
  h2 {{ font-weight: 600; font-size: 1.4rem; margin: 3rem 0 .5rem; }}
  h3 {{ font-weight: 600; font-size: 1.1rem; margin: 2rem 0 .5rem; }}
  .headerlink {{ margin-left: .4rem; color: var(--ink-2); text-decoration: none; opacity: 0; }}
  :is(h2, h3):hover .headerlink, .headerlink:focus-visible {{ opacity: 1; }}
  nav.toc {{ background: var(--surface); border: 1px solid var(--line); border-radius: 14px; padding: .75rem 1.25rem; margin: 1.5rem 0; }}
  nav.toc ul {{ margin: .25rem 0; padding-left: 1.25rem; }}
  nav.toc li {{ margin: .15rem 0; }}
  .table {{ overflow-x: auto; margin: 1rem 0; }}
  table {{ border-collapse: collapse; font-size: .95rem; }}
  th, td {{ border: 1px solid var(--line); padding: .4rem .6rem; text-align: left; vertical-align: top; }}
  th {{ background: var(--surface); font-weight: 600; }}
  li {{ margin-bottom: .4rem; }}
  footer {{ border-top: 1px solid var(--line); padding: 2rem 0 3rem; color: var(--ink-2); font-size: .92rem; }}
</style>
</head>
<body>
<div class="wrap">
  <header class="top">
    <a class="brand" href="../">
      <img src="../brand/ichor.svg" width="32" height="32" alt="">
      Ichor
    </a>
  </header>

  <main translate="no">
{body}
  </main>

  <footer>
    <p>Generated from <a href="{repo}{name}.md">docs/{name}.md</a>.</p>
  </footer>
</div>
</body>
</html>
"""


def render(name: str, source: str) -> str:
    """The page for the Markdown text: its first heading as title, a table of contents
    after the introduction, wide tables scrolling on their own."""
    md = markdown.Markdown(
        extensions=["fenced_code", "tables", "toc"],
        extension_configs={"toc": {"permalink": "#", "toc_depth": "2-3"}},
    )
    body = md.convert(source)
    title_match = re.search(r"<h1[^>]*>(.*?)</h1>", body, re.S)
    if not title_match:
        raise ValueError(f"{name}.md has no top-level heading")
    title = re.sub(r"<[^>]+>", "", title_match.group(1)).replace("#", "").strip()
    intro = re.search(r"<p>(.*?)</p>", body, re.S)
    description = re.sub(r"\s+", " ", re.sub(r"<[^>]+>", "", intro.group(1))).strip() if intro else title
    first_section = body.find("<h2")
    if first_section >= 0:
        toc = f'<nav class="toc" aria-label="Contents">{md.toc}</nav>\n'
        body = body[:first_section] + toc + body[first_section:]
    # A table cell's `a \| b` is `a | b` on GitHub; Markdown keeps the backslash in code.
    body = re.sub(r"<(td|th)>(.*?)</\1>", lambda m: m.group(0).replace("\\|", "|"), body, flags=re.S)
    body = re.sub(r"<table>", '<div class="table"><table>', body)
    body = re.sub(r"</table>", "</table></div>", body)
    return TEMPLATE.format(
        title=html.escape(title),
        description=html.escape(description.split(". ")[0].rstrip(".") + ".", quote=True),
        site=SITE,
        name=name,
        repo=REPO,
        body=body,
    )


def main() -> int:
    for name in PAGES:
        try:
            page = render(name, (DOCS / f"{name}.md").read_text(encoding="utf-8"))
        except (OSError, ValueError) as error:
            print(f"site-reference: {error}", file=sys.stderr)
            return 1
        output = DOCS / name / "index.html"
        output.parent.mkdir(parents=True, exist_ok=True)
        output.write_text(page, encoding="utf-8")
        print(f"wrote {output.relative_to(ROOT)}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
