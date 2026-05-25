#!/usr/bin/env python3

import csv
import json
import re
import sys
import zipfile
from html.parser import HTMLParser
from pathlib import Path
from xml.etree import ElementTree


PROJECT_ROOT = Path(__file__).resolve().parent.parent
UNSTRUCTURED_SOURCE = PROJECT_ROOT / "repos" / "deps" / "upstream" / "adoption_sources" / "document" / "unstructured"
MARKER_SOURCE = PROJECT_ROOT / "repos" / "deps" / "upstream" / "adoption_sources" / "document" / "marker"
TEXT_EXTENSIONS = {
    ".txt",
    ".md",
    ".markdown",
    ".json",
    ".jsonl",
    ".yaml",
    ".yml",
    ".toml",
    ".ini",
    ".cfg",
    ".conf",
    ".csv",
    ".tsv",
    ".py",
    ".go",
    ".js",
    ".ts",
    ".tsx",
    ".jsx",
    ".java",
    ".rs",
    ".rb",
    ".sh",
    ".zsh",
    ".bash",
    ".sql",
    ".xml",
    ".log",
}


class SimpleTextHTMLParser(HTMLParser):
    def __init__(self):
        super().__init__()
        self.parts = []

    def handle_data(self, data):
        text = re.sub(r"\s+", " ", data or "").strip()
        if text:
            self.parts.append(text)

    @property
    def text(self):
        return re.sub(r"\s+", " ", " ".join(self.parts)).strip()


def make_response(status, summary="", error="", artifacts=None, data=None):
    return {
        "status": status,
        "summary": summary,
        "error": error,
        "artifacts": artifacts or {},
        "data": data or {},
    }


def normalize_text(value, max_chars):
    text = re.sub(r"\s+", " ", value or "").strip()
    truncated = False
    if max_chars > 0 and len(text) > max_chars:
        text = text[:max_chars]
        truncated = True
    return text, truncated


def extract_builtin_text(path, max_chars):
    text = path.read_text(encoding="utf-8", errors="ignore")
    text, truncated = normalize_text(text, max_chars)
    return {"backend": "builtin_text", "text": text, "truncated": truncated}


def extract_builtin_html(path, max_chars):
    parser = SimpleTextHTMLParser()
    parser.feed(path.read_text(encoding="utf-8", errors="ignore"))
    text, truncated = normalize_text(parser.text, max_chars)
    return {"backend": "builtin_html", "text": text, "truncated": truncated}


def extract_builtin_csv(path, max_chars):
    lines = []
    with path.open("r", encoding="utf-8", errors="ignore", newline="") as handle:
        reader = csv.reader(handle)
        for row in reader:
            if row:
                lines.append(" | ".join(cell.strip() for cell in row))
    text, truncated = normalize_text("\n".join(lines), max_chars)
    return {"backend": "builtin_csv", "text": text, "truncated": truncated}


def extract_builtin_docx(path, max_chars):
    with zipfile.ZipFile(path, "r") as archive:
        xml_text = archive.read("word/document.xml")
    root = ElementTree.fromstring(xml_text)
    texts = []
    for node in root.iter():
        if node.tag.endswith("}t") and node.text:
            texts.append(node.text)
    text, truncated = normalize_text(" ".join(texts), max_chars)
    return {"backend": "builtin_docx", "text": text, "truncated": truncated}


def try_unstructured(path, max_chars):
    if str(UNSTRUCTURED_SOURCE) not in sys.path:
        sys.path.insert(0, str(UNSTRUCTURED_SOURCE))
    try:
        from unstructured.partition.auto import partition
    except Exception as exc:
        raise RuntimeError(f"unstructured import failed: {exc}") from exc
    elements = partition(filename=str(path))
    combined = "\n\n".join(str(item).strip() for item in elements if str(item).strip())
    text, truncated = normalize_text(combined, max_chars)
    return {"backend": "unstructured", "text": text, "truncated": truncated}


def extract_document(path, max_chars, prefer_backend):
    suffix = path.suffix.lower()
    warnings = []
    backends = ["builtin"]
    if UNSTRUCTURED_SOURCE.exists():
        backends.append("unstructured_source_present")
    if MARKER_SOURCE.exists():
        backends.append("marker_source_present")

    if prefer_backend == "unstructured":
        try:
            payload = try_unstructured(path, max_chars)
            payload["warnings"] = warnings
            payload["available_backends"] = backends
            return payload
        except Exception as exc:
            warnings.append(str(exc))

    if suffix in TEXT_EXTENSIONS:
        payload = extract_builtin_text(path, max_chars)
    elif suffix in {".html", ".htm"}:
        payload = extract_builtin_html(path, max_chars)
    elif suffix in {".csv", ".tsv"}:
        payload = extract_builtin_csv(path, max_chars)
    elif suffix == ".docx":
        payload = extract_builtin_docx(path, max_chars)
    elif suffix in {".pdf", ".doc", ".pptx", ".xlsx"}:
        try:
            payload = try_unstructured(path, max_chars)
        except Exception as exc:
            raise RuntimeError(
                "No usable document parser is available for this file type. "
                f"Unstructured source is present at {UNSTRUCTURED_SOURCE}, but runtime parsing failed: {exc}"
            ) from exc
    else:
        payload = extract_builtin_text(path, max_chars)

    payload["warnings"] = warnings
    payload["available_backends"] = backends
    return payload


def main():
    try:
        payload = json.loads(sys.stdin.read() or "{}")
    except Exception as exc:
        print(json.dumps(make_response("error", error=f"invalid input: {exc}"), ensure_ascii=False))
        return

    raw_path = str(payload.get("path", "")).strip()
    if not raw_path:
        print(json.dumps(make_response("error", error="missing path"), ensure_ascii=False))
        return

    path = Path(raw_path).expanduser()
    max_chars = int(payload.get("max_chars") or 12000)
    prefer_backend = str(payload.get("prefer_backend", "auto")).strip().lower() or "auto"
    if not path.exists() or not path.is_file():
        print(json.dumps(make_response("error", error=f"document does not exist: {path}"), ensure_ascii=False))
        return

    try:
        extracted = extract_document(path, max_chars, prefer_backend)
    except Exception as exc:
        print(json.dumps(make_response("error", error=str(exc)), ensure_ascii=False))
        return

    print(
        json.dumps(
            make_response(
                "success",
                summary=f"Extracted readable content from {path.name} using {extracted['backend']}.",
                artifacts={
                    "path": str(path),
                    "backend": str(extracted.get("backend", "")),
                    "unstructured_source": str(UNSTRUCTURED_SOURCE),
                    "marker_source": str(MARKER_SOURCE),
                },
                data={
                    "path": str(path),
                    "file_name": path.name,
                    "file_type": path.suffix.lower(),
                    "backend": extracted.get("backend"),
                    "text": extracted.get("text", ""),
                    "truncated": bool(extracted.get("truncated")),
                    "warnings": extracted.get("warnings", []),
                    "available_backends": extracted.get("available_backends", []),
                },
            ),
            ensure_ascii=False,
        )
    )


if __name__ == "__main__":
    main()
