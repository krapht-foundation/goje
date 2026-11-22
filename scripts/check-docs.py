"""Check local inline links in repository Markdown documents."""

from __future__ import annotations

from pathlib import Path
import re
import sys
from urllib.parse import unquote, urlsplit

ROOT = Path(__file__).resolve().parent.parent
LINK = re.compile(r"!?\[[^\]\n]+\]\(([^\s)]+)\)")
HEADING = re.compile(r"^ {0,3}#{1,6}\s+(.+?)\s*#*\s*$", re.MULTILINE)


def anchors(path: Path) -> set[str]:
    """Return heading slugs for a Markdown file, suffixing duplicate slugs."""
    seen = set()
    for heading in HEADING.findall(path.read_text(encoding="utf-8")):
        base = re.sub(r"[^\w -]", "", heading.lower()).replace(" ", "-")
        slug, suffix = base, 0
        while slug in seen:
            suffix += 1
            slug = f"{base}-{suffix}"
        seen.add(slug)
    return seen


def main() -> bool:
    """Report broken local links and return whether any were found."""
    docs = sorted(ROOT.glob("*.md"))
    for directory in (".ai", "decisions", "docs", "rfcs", "runbook", "specs"):
        docs.extend(sorted((ROOT / directory).rglob("*.md")))
    failures = 0
    for doc in docs:
        for line, text in enumerate(doc.read_text(encoding="utf-8").splitlines(), 1):
            for target in LINK.findall(text):
                url = urlsplit(target)
                if url.scheme or url.netloc:
                    continue
                destination = doc.parent / unquote(url.path) if url.path else doc
                if destination.is_dir() and url.fragment:
                    destination /= "README.md"
                if not destination.exists() or (
                    url.fragment and destination.suffix == ".md"
                    and unquote(url.fragment) not in anchors(destination)
                ):
                    print(f"{doc.relative_to(ROOT)}:{line}: broken link: {target}", file=sys.stderr)
                    failures += 1
    if not failures:
        print(f"Checked local links in {len(docs)} Markdown files")
    return bool(failures)


if __name__ == "__main__":
    sys.exit(main())
