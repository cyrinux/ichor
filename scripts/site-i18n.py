#!/usr/bin/env python3
"""Builds the translated website (docs/<lang>/) from the English pages in docs/.

The English pages stay the hand-edited source. Their translatable strings are found
without keys: every heading, paragraph, list item, table cell, summary, caption and
button with no block element inside, every element marked `data-i18n`, plus the `alt`
and `aria-label` attributes and those named in `data-i18n-attr`. `translate="no"`
excludes an element and everything in it. Each string is looked up, whitespace
collapsed, in docs-i18n/<lang>.json ({"English": "translation"}); a missing one stays
in English.

    scripts/site-i18n.py          write docs/<lang>/*.html, warn about missing strings
    scripts/site-i18n.py --sync   add missing strings (as null) to every docs-i18n file,
                                  drop the ones the pages no longer use
    scripts/site-i18n.py --check  fail on a missing, empty or unused translation
"""
import json
import re
import sys
from html.parser import HTMLParser
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
DOCS = ROOT / "docs"
STRINGS = ROOT / "docs-i18n"
PAGES = ["index.html", "privacy.html"]
LANGUAGES = ["fr", "es", "uk", "de", "it"]
SITE = "https://cyrinux.github.io/ichor/"

# A string unit: one of these with no UNIT or CONTAINER element inside.
UNIT = {"title", "h1", "h2", "h3", "h4", "p", "li", "th", "td", "summary", "figcaption",
        "blockquote", "button", "label", "caption", "dt", "dd", "option"}
CONTAINER = {"div", "ul", "ol", "table", "section", "article", "figure", "details", "nav",
             "header", "footer", "main", "pre", "form"}
VOID = {"area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta",
        "source", "track", "wbr"}
AUTO_ATTRS = ("alt", "aria-label")
LETTER = re.compile(r"[^\W\d_]")


def normalize(text: str) -> str:
    return re.sub(r"\s+", " ", text).strip()


def has_words(html: str) -> bool:
    return bool(LETTER.search(re.sub(r"<[^>]*>", "", html)))


class Extractor(HTMLParser):
    """Finds the translatable spans of a page: (start, end, kind, english)."""

    def __init__(self, source: str):
        super().__init__(convert_charrefs=False)
        self.source = source
        self.line_starts = [0]
        for match in re.finditer("\n", source):
            self.line_starts.append(match.end())
        self.stack: list[dict] = []
        self.spans: list[tuple[int, int, str, str]] = []

    def position(self) -> int:
        line, column = self.getpos()
        return self.line_starts[line - 1] + column

    def skipping(self) -> bool:
        return any(entry["no"] for entry in self.stack)

    def handle_starttag(self, tag, attrs):
        self.start(tag, attrs, closes=tag in VOID)

    def handle_startendtag(self, tag, attrs):
        self.start(tag, attrs, closes=True)

    def start(self, tag, attrs, closes):
        attributes = dict(attrs)
        start = self.position()
        text = self.get_starttag_text()
        no = attributes.get("translate") == "no" or self.skipping()
        if not no:
            names = list(AUTO_ATTRS) + (attributes.get("data-i18n-attr") or "").split()
            for name in names:
                pattern = re.compile(r"(\s" + re.escape(name) + r"\s*=\s*)([\"'])(.*?)\2", re.S)
                match = pattern.search(text)
                if match and has_words(match.group(3)):
                    self.spans.append((start + match.start(3), start + match.end(3), "attr",
                                       match.group(3)))
        if closes:
            return
        if self.stack and (tag in UNIT or tag in CONTAINER):
            self.stack[-1]["blocks"] = True
        self.stack.append({"tag": tag, "inner": start + len(text), "blocks": False,
                           "explicit": "data-i18n" in attributes, "no": no})

    def handle_endtag(self, tag):
        if not any(entry["tag"] == tag for entry in self.stack):
            return
        end = self.position()
        while True:
            entry = self.stack.pop()
            if entry["tag"] == tag:
                break
        if entry["no"] or not (entry["explicit"] or (tag in UNIT and not entry["blocks"])):
            return
        if entry["explicit"] and entry["blocks"]:
            raise SystemExit(f"data-i18n on a <{tag}> that holds another string, at offset {end}")
        inner = self.source[entry["inner"]:end]
        if not has_words(inner):
            return
        if any(entry["inner"] <= s and e <= end for s, e, _, _ in self.spans):
            raise SystemExit(f"<{tag}> at offset {end} overlaps a translated attribute")
        self.spans.append((entry["inner"], end, "text", inner))
        if self.stack:
            self.stack[-1]["blocks"] = True


def extract(source: str) -> list[tuple[int, int, str, str]]:
    parser = Extractor(source)
    parser.feed(source)
    parser.close()
    return sorted(parser.spans)


def localize_urls(html: str, lang: str, page: str) -> str:
    """Rewrites the page for docs/<lang>/: assets one level up, language links fixed."""
    def language_link(match: re.Match) -> str:
        tag = match.group(0)
        target = re.search(r'hreflang="([^"]+)"', tag).group(1)
        path = "" if target == "en" else f"{target}/"
        href = "../" + path + ("" if page == "index.html" else page)
        tag = re.sub(r'href="[^"]*"', f'href="{href or "./"}"', tag)
        tag = tag.replace(' aria-current="true"', "")
        if target == lang:
            tag = tag.replace("<a ", '<a aria-current="true" ', 1)
        return tag

    def asset(match: re.Match) -> str:
        url = match.group(2)
        local = url.startswith("#") or url in ("./", *PAGES)
        if local or re.match(r"^[a-z][a-z0-9+.-]*:|^/", url):
            return match.group(0)
        return f'{match.group(1)}="../{url}"'

    html = re.sub(r'\b(href|src)="([^"]+)"', asset, html)
    html = re.sub(r"<a [^>]*hreflang=\"[^\"]+\"[^>]*>", language_link, html)
    html = re.sub(r'<html lang="en"', f'<html lang="{lang}"', html, count=1)
    page_url = SITE + f"{lang}/" + ("" if page == "index.html" else page)
    html = re.sub(r'(<link rel="canonical" href=")[^"]*', r"\g<1>" + page_url, html)
    html = re.sub(r'(<meta property="og:url" content=")[^"]*', r"\g<1>" + page_url, html)
    return html


def render(source: str, spans, table: dict[str, str | None], missing: set[str]) -> str:
    parts, last = [], 0
    for start, end, _, english in spans:
        key = normalize(english)
        translated = table.get(key)
        if not translated:
            missing.add(key)
        parts.append(source[last:start])
        parts.append(translated or english)
        last = end
    parts.append(source[last:])
    return "".join(parts)


def load(lang: str) -> dict[str, str | None]:
    path = STRINGS / f"{lang}.json"
    return json.loads(path.read_text()) if path.exists() else {}


def main() -> int:
    mode = sys.argv[1] if len(sys.argv) > 1 else "build"
    if mode not in ("build", "--sync", "--check"):
        print(__doc__, file=sys.stderr)
        return 2
    sources = {page: (DOCS / page).read_text() for page in PAGES}
    spans = {page: extract(text) for page, text in sources.items()}
    keys = list(dict.fromkeys(normalize(s[3]) for page in PAGES for s in spans[page]))

    problems = 0
    for lang in LANGUAGES:
        table = load(lang)
        if mode == "--sync":
            synced = {key: table.get(key) for key in keys}
            STRINGS.mkdir(exist_ok=True)
            (STRINGS / f"{lang}.json").write_text(
                json.dumps(synced, ensure_ascii=False, indent=2) + "\n")
            todo = sum(1 for value in synced.values() if not value)
            print(f"{lang}: {len(keys)} strings, {todo} to translate")
            continue
        missing: set[str] = set()
        for page in PAGES:
            html = render(sources[page], spans[page], table, missing)
            if mode == "build":
                out = DOCS / lang / page
                out.parent.mkdir(exist_ok=True)
                out.write_text(localize_urls(html, lang, page))
        unused = [key for key in table if key not in keys]
        for key in sorted(missing):
            print(f"{'::error::' if mode == '--check' else '::warning::'}site {lang}: "
                  f"untranslated {key[:90]!r}", file=sys.stderr)
        if mode == "--check":
            for key in unused:
                print(f"::error::site {lang}: unused {key[:90]!r}", file=sys.stderr)
            problems += len(missing) + len(unused)
        elif missing:
            print(f"{lang}: {len(missing)} strings left in English", file=sys.stderr)
    if mode == "--check" and not problems:
        print(f"website: {len(keys)} strings translated into {', '.join(LANGUAGES)}")
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main())
