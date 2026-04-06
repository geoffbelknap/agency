# Knowledge Graph Intelligence — Phase 2b: Extended Extractors & Watch Mode

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add HTML, code, and PDF extractors to the universal ingestion pipeline, plus a directory watch mode for auto-ingestion and image ingestion via LLM vision.

**Architecture:** Each extractor extends `BaseExtractor` from Phase 2. All are optional-dependency-aware — they degrade gracefully if their deps aren't installed (BeautifulSoup for HTML, tree-sitter for code, PyMuPDF for PDF, watchdog for directory watching). The pipeline auto-discovers available extractors at init time.

**Tech Stack:** Python, html.parser (stdlib), beautifulsoup4 (optional), tree-sitter (optional), pymupdf (optional), watchdog (optional)

**Spec:** `docs/specs/knowledge-graph-intelligence.md` — Phase 2 section

**Depends on:** Phase 2 (ingestion pipeline framework) — completed

---

## File Structure

### Files to Create

| File | Purpose |
|------|---------|
| `images/knowledge/ingestion/extractors/html_extractor.py` | HTML/web page extractor — titles, links, headings, metadata |
| `images/knowledge/ingestion/extractors/code.py` | Code file extractor — functions, classes, imports via tree-sitter or regex fallback |
| `images/knowledge/ingestion/extractors/pdf.py` | PDF text extraction via PyMuPDF with stdlib fallback |
| `images/knowledge/ingestion/watcher.py` | Directory watch mode — auto-ingest files dropped in watch dir |
| `images/tests/test_html_extractor.py` | Tests for HTML extractor |
| `images/tests/test_code_extractor.py` | Tests for code extractor |
| `images/tests/test_pdf_extractor.py` | Tests for PDF extractor |
| `images/tests/test_watcher.py` | Tests for watch mode |

### Files to Modify

| File | Changes |
|------|---------|
| `images/knowledge/ingestion/pipeline.py` | Register new extractors, add image handling path |
| `images/knowledge/ingestion/classifier.py` | Add PDF binary detection |
| `images/knowledge/server.py` | Start/stop watcher lifecycle, `/ingest` accept binary |
| `images/knowledge/requirements.txt` | Add optional deps with extras |

---

## Task 1: HTML Extractor

**Files:**
- Create: `images/knowledge/ingestion/extractors/html_extractor.py`
- Test: `images/tests/test_html_extractor.py`

- [ ] **Step 1: Write failing tests**

```python
# images/tests/test_html_extractor.py
"""Tests for HTML/web page extractor."""
import os
import sys
import pytest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "knowledge"))

from ingestion.extractors.html_extractor import HtmlExtractor


class TestHtmlExtractorCanHandle:
    def test_handles_html(self):
        assert HtmlExtractor().can_handle("text/html")

    def test_does_not_handle_markdown(self):
        assert not HtmlExtractor().can_handle("text/markdown")


class TestHtmlExtractorTitle:
    def test_extracts_title(self):
        html = "<html><head><title>Security Advisory</title></head><body><p>Details</p></body></html>"
        result = HtmlExtractor().extract(html, filename="advisory.html")
        titles = [n for n in result.nodes if n.get("kind") == "document"]
        assert any("Security Advisory" in n["label"] for n in titles)


class TestHtmlExtractorHeadings:
    def test_extracts_headings(self):
        html = "<html><body><h1>Overview</h1><p>Text</p><h2>Details</h2><p>More</p></body></html>"
        result = HtmlExtractor().extract(html)
        labels = [n["label"] for n in result.nodes]
        assert "Overview" in labels
        assert "Details" in labels


class TestHtmlExtractorLinks:
    def test_extracts_links(self):
        html = '<html><body><a href="https://example.com/docs">Docs</a></body></html>'
        result = HtmlExtractor().extract(html)
        url_nodes = [n for n in result.nodes if n.get("kind") == "url"]
        assert any("example.com" in n["label"] for n in url_nodes)

    def test_skips_anchor_links(self):
        html = '<html><body><a href="#section">Jump</a></body></html>'
        result = HtmlExtractor().extract(html)
        url_nodes = [n for n in result.nodes if n.get("kind") == "url"]
        assert len(url_nodes) == 0


class TestHtmlExtractorMeta:
    def test_extracts_meta_description(self):
        html = '<html><head><meta name="description" content="A security tool"></head><body></body></html>'
        result = HtmlExtractor().extract(html)
        assert "description" in str(result.metadata) or any("security" in str(n) for n in result.nodes)


class TestHtmlExtractorSynthesis:
    def test_needs_synthesis_for_content(self):
        html = "<html><body><p>" + "This is a detailed analysis. " * 20 + "</p></body></html>"
        result = HtmlExtractor().extract(html)
        assert result.needs_synthesis is True

    def test_minimal_html_no_synthesis(self):
        html = "<html><body><p>Hi</p></body></html>"
        result = HtmlExtractor().extract(html)
        assert result.needs_synthesis is False
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/geoff/agency-workspace/agency && python3 -m pytest images/tests/test_html_extractor.py -v`

- [ ] **Step 3: Implement HtmlExtractor**

Use `html.parser.HTMLParser` (stdlib) as the primary parser. This avoids requiring BeautifulSoup. Extract:
- `<title>` → `document` node
- `<h1>`-`<h6>` → `concept` nodes (same as markdown extractor)
- `<a href="...">` → `url` nodes (skip anchor-only links starting with #)
- `<meta name="description" content="...">` → store in metadata
- Text content between tags → `raw_content` for synthesis decision
- `needs_synthesis` based on text content length (>200 chars)

```python
# images/knowledge/ingestion/extractors/html_extractor.py
"""Deterministic extractor for HTML and web pages.

Uses stdlib html.parser — no external dependencies required.
Optionally uses BeautifulSoup if available for better parsing.
"""
import re
from html.parser import HTMLParser
from typing import Optional

from ingestion.base import ExtractionResult, BaseExtractor

_SYNTHESIS_THRESHOLD = 200


class _ContentParser(HTMLParser):
    """Simple HTML parser that extracts structural elements."""

    def __init__(self):
        super().__init__()
        self.title = ""
        self.headings = []
        self.links = []
        self.meta = {}
        self.text_parts = []
        self._current_tag = ""
        self._in_title = False
        self._heading_level = 0

    def handle_starttag(self, tag, attrs):
        self._current_tag = tag
        attrs_dict = dict(attrs)
        if tag == "title":
            self._in_title = True
        elif tag in ("h1", "h2", "h3", "h4", "h5", "h6"):
            self._heading_level = int(tag[1])
        elif tag == "a":
            href = attrs_dict.get("href", "")
            if href and not href.startswith("#") and not href.startswith("javascript:"):
                self.links.append(href)
        elif tag == "meta":
            name = attrs_dict.get("name", "")
            content = attrs_dict.get("content", "")
            if name and content:
                self.meta[name] = content

    def handle_endtag(self, tag):
        if tag == "title":
            self._in_title = False
        if tag in ("h1", "h2", "h3", "h4", "h5", "h6"):
            self._heading_level = 0
        self._current_tag = ""

    def handle_data(self, data):
        text = data.strip()
        if not text:
            return
        if self._in_title:
            self.title = text
        elif self._heading_level:
            self.headings.append({"text": text, "level": self._heading_level})
        if text:
            self.text_parts.append(text)


class HtmlExtractor(BaseExtractor):
    """Extracts structure from HTML: title, headings, links, metadata."""

    @property
    def name(self) -> str:
        return "html"

    def can_handle(self, content_type: str, filename: str = "") -> bool:
        return content_type == "text/html"

    def extract(self, content: str, filename: str = "",
                metadata: Optional[dict] = None) -> ExtractionResult:
        parser = _ContentParser()
        try:
            parser.feed(content)
        except Exception:
            pass  # Best-effort parsing

        nodes = []
        edges = []

        # Title → document node
        if parser.title:
            nodes.append({
                "label": parser.title,
                "kind": "document",
                "summary": f"Web page title from {filename or 'HTML document'}",
                "properties": {"source_file": filename},
            })

        # Headings → concept nodes
        for h in parser.headings:
            nodes.append({
                "label": h["text"],
                "kind": "concept",
                "summary": f"Section heading (h{h['level']}) from {filename or 'HTML document'}",
                "properties": {"heading_level": h["level"], "source_file": filename},
            })

        # Links → url nodes
        seen_links = set()
        for href in parser.links:
            if href in seen_links:
                continue
            seen_links.add(href)
            if href.startswith(("http://", "https://")):
                nodes.append({
                    "label": href,
                    "kind": "url",
                    "summary": f"Link from {filename or 'HTML document'}",
                    "properties": {"source_file": filename},
                })

        # Determine synthesis need
        text_content = " ".join(parser.text_parts)
        needs_synthesis = len(text_content) > _SYNTHESIS_THRESHOLD

        return ExtractionResult(
            source_type="html",
            content_type="text/html",
            nodes=nodes,
            edges=edges,
            raw_content=text_content if needs_synthesis else "",
            needs_synthesis=needs_synthesis,
            metadata={
                "source_file": filename,
                "title": parser.title,
                **parser.meta,
                **(metadata or {}),
            },
        )
```

- [ ] **Step 4: Run tests to verify they pass**

- [ ] **Step 5: Commit**

```bash
git add images/knowledge/ingestion/extractors/html_extractor.py images/tests/test_html_extractor.py
git commit -m "feat(knowledge): add HTML extractor for web page ingestion

Stdlib html.parser — no external deps. Extracts title, headings,
links, and meta description. Synthesis triggered for prose-heavy pages."
```

---

## Task 2: Code Extractor (Regex Fallback + Optional Tree-sitter)

**Files:**
- Create: `images/knowledge/ingestion/extractors/code.py`
- Test: `images/tests/test_code_extractor.py`

- [ ] **Step 1: Write failing tests**

```python
# images/tests/test_code_extractor.py
"""Tests for code file extractor."""
import os
import sys
import pytest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "knowledge"))

from ingestion.extractors.code import CodeExtractor


class TestCodeExtractorCanHandle:
    def test_handles_python(self):
        assert CodeExtractor().can_handle("text/x-python")

    def test_handles_go(self):
        assert CodeExtractor().can_handle("text/x-go")

    def test_handles_javascript(self):
        assert CodeExtractor().can_handle("text/javascript")

    def test_does_not_handle_markdown(self):
        assert not CodeExtractor().can_handle("text/markdown")


class TestCodeExtractorPython:
    def test_extracts_functions(self):
        code = '''
def authenticate(username, password):
    """Check user credentials."""
    return check_db(username, password)

def authorize(user, resource):
    """Check permissions."""
    return has_permission(user, resource)
'''
        result = CodeExtractor().extract(code, filename="auth.py")
        labels = [n["label"] for n in result.nodes]
        assert "authenticate" in labels
        assert "authorize" in labels

    def test_extracts_classes(self):
        code = '''
class AuthService:
    """Authentication service."""
    def login(self):
        pass

class UserManager:
    """User management."""
    pass
'''
        result = CodeExtractor().extract(code, filename="services.py")
        labels = [n["label"] for n in result.nodes]
        assert "AuthService" in labels
        assert "UserManager" in labels

    def test_extracts_imports(self):
        code = '''
import os
from pathlib import Path
from .auth import AuthService
'''
        result = CodeExtractor().extract(code, filename="main.py")
        # Imports should create edges or be noted in metadata
        assert len(result.nodes) > 0 or len(result.edges) > 0 or "imports" in str(result.metadata)


class TestCodeExtractorGo:
    def test_extracts_functions(self):
        code = '''
package auth

func Authenticate(username, password string) bool {
    return checkDB(username, password)
}

func Authorize(user *User, resource string) error {
    return hasPermission(user, resource)
}
'''
        result = CodeExtractor().extract(code, filename="auth.go")
        labels = [n["label"] for n in result.nodes]
        assert "Authenticate" in labels
        assert "Authorize" in labels

    def test_extracts_structs(self):
        code = '''
package models

type User struct {
    Name string
    Email string
}

type Session struct {
    Token string
    UserID int
}
'''
        result = CodeExtractor().extract(code, filename="models.go")
        labels = [n["label"] for n in result.nodes]
        assert "User" in labels
        assert "Session" in labels


class TestCodeExtractorJavaScript:
    def test_extracts_functions(self):
        code = '''
function fetchUsers() {
    return fetch('/api/users');
}

const processData = (data) => {
    return data.filter(x => x.active);
};

export class ApiClient {
    constructor(baseUrl) {
        this.baseUrl = baseUrl;
    }
}
'''
        result = CodeExtractor().extract(code, filename="api.js")
        labels = [n["label"] for n in result.nodes]
        assert "fetchUsers" in labels
        assert "ApiClient" in labels


class TestCodeExtractorSynthesis:
    def test_code_with_docstrings_needs_synthesis(self):
        """Code with substantial docstrings/comments should trigger synthesis."""
        code = '''
def complex_analysis():
    """
    This function performs a multi-step security analysis including
    vulnerability scanning, configuration auditing, and compliance
    checking against industry standards like CIS benchmarks.
    It correlates findings across multiple data sources.
    """
    pass
'''
        result = CodeExtractor().extract(code, filename="analysis.py")
        # Substantial docstrings → needs synthesis for semantic extraction
        assert result.needs_synthesis is True

    def test_code_without_comments_may_skip(self):
        """Pure code without comments/docs has less need for synthesis."""
        code = "def add(a, b):\n    return a + b\n"
        result = CodeExtractor().extract(code, filename="math.py")
        # Minimal code — deterministic extraction may be sufficient
        # (needs_synthesis depends on comment/docstring volume)


class TestCodeExtractorProvenance:
    def test_default_provenance_is_extracted(self):
        code = "def hello(): pass"
        result = CodeExtractor().extract(code, filename="hello.py")
        assert result.default_provenance == "EXTRACTED"
```

- [ ] **Step 2: Run tests to verify they fail**

- [ ] **Step 3: Implement CodeExtractor**

Use regex-based parsing for broad language support without external deps. Patterns per language:

**Python:**
- Functions: `^def\s+(\w+)\s*\(` → `function` nodes
- Classes: `^class\s+(\w+)` → `class` nodes (mapped to `system` ontology type)
- Imports: `^import\s+(\S+)` and `^from\s+(\S+)\s+import` → import edges
- Docstrings: `"""..."""` → include in synthesis decision

**Go:**
- Functions: `^func\s+(?:\([^)]*\)\s+)?(\w+)\s*\(` → `function` nodes
- Structs: `^type\s+(\w+)\s+struct` → `system` nodes
- Interfaces: `^type\s+(\w+)\s+interface` → `system` nodes
- Imports: `"([^"]+)"` inside import blocks → import edges

**JavaScript/TypeScript:**
- Functions: `^(?:export\s+)?function\s+(\w+)` and `^(?:export\s+)?(?:const|let|var)\s+(\w+)\s*=\s*(?:\([^)]*\)\s*=>|function)` → `function` nodes
- Classes: `^(?:export\s+)?class\s+(\w+)` → `system` nodes

**Synthesis decision:** Count total comment/docstring characters. If >200 chars of documentation, set `needs_synthesis=True` (the docs may describe semantic relationships worth extracting).

```python
# images/knowledge/ingestion/extractors/code.py
"""Deterministic extractor for source code files.

Uses regex-based parsing for broad language support without external
dependencies. Extracts functions, classes/structs, and import relationships.
Optionally enhanced by tree-sitter if available.
"""
import re
from typing import Optional

from ingestion.base import ExtractionResult, BaseExtractor
from ingestion.classifier import SourceClassifier

# ... regex patterns per language ...
# ... CodeExtractor implementation ...
```

The extractor should:
1. Detect language from content_type
2. Apply language-specific regex patterns
3. Create `function` nodes for functions/methods
4. Create `system` nodes for classes/structs/interfaces (mapped from ontology)
5. Create `depends_on` edges for imports
6. Set `needs_synthesis` based on comment/docstring volume
7. Store filename, language, line counts in metadata

- [ ] **Step 4: Run tests to verify they pass**

- [ ] **Step 5: Commit**

```bash
git add images/knowledge/ingestion/extractors/code.py images/tests/test_code_extractor.py
git commit -m "feat(knowledge): add code extractor with regex parsing for Python/Go/JS

Extracts functions, classes, structs, imports. Synthesis triggered
when docstrings/comments exceed threshold. No external deps required."
```

---

## Task 3: PDF Extractor

**Files:**
- Create: `images/knowledge/ingestion/extractors/pdf.py`
- Test: `images/tests/test_pdf_extractor.py`

- [ ] **Step 1: Write failing tests**

```python
# images/tests/test_pdf_extractor.py
"""Tests for PDF extractor."""
import os
import sys
import pytest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "knowledge"))

from ingestion.extractors.pdf import PdfExtractor


class TestPdfExtractorCanHandle:
    def test_handles_pdf(self):
        assert PdfExtractor().can_handle("application/pdf")

    def test_does_not_handle_text(self):
        assert not PdfExtractor().can_handle("text/plain")


class TestPdfExtractorAvailability:
    def test_reports_availability(self):
        """Extractor should report whether PyMuPDF is available."""
        ext = PdfExtractor()
        # available is True if pymupdf is installed, False otherwise
        assert isinstance(ext.available, bool)


class TestPdfExtractorFallback:
    def test_unavailable_returns_empty_with_note(self):
        """When PyMuPDF isn't installed, return empty result with metadata note."""
        ext = PdfExtractor()
        if not ext.available:
            result = ext.extract("fake pdf content", filename="report.pdf")
            assert len(result.nodes) == 0
            assert "unavailable" in str(result.metadata).lower() or "pymupdf" in str(result.metadata).lower()
            # Raw content should still be set for synthesis attempt
            assert result.needs_synthesis is True
```

- [ ] **Step 2: Implement PdfExtractor**

```python
# images/knowledge/ingestion/extractors/pdf.py
"""PDF text extraction for knowledge graph ingestion.

Requires PyMuPDF (fitz) for full extraction. Without it, degrades
gracefully — returns empty extraction with a metadata note and
flags needs_synthesis=True so the LLM can attempt vision-based extraction.
"""
from typing import Optional

from ingestion.base import ExtractionResult, BaseExtractor

try:
    import fitz  # PyMuPDF
    _PYMUPDF_AVAILABLE = True
except ImportError:
    _PYMUPDF_AVAILABLE = False


class PdfExtractor(BaseExtractor):
    """Extracts text and structure from PDF documents."""

    @property
    def name(self) -> str:
        return "pdf"

    @property
    def available(self) -> bool:
        return _PYMUPDF_AVAILABLE

    def can_handle(self, content_type: str, filename: str = "") -> bool:
        return content_type == "application/pdf"

    def extract(self, content: str, filename: str = "",
                metadata: Optional[dict] = None) -> ExtractionResult:
        if not _PYMUPDF_AVAILABLE:
            return ExtractionResult(
                source_type="pdf",
                content_type="application/pdf",
                raw_content=content,
                needs_synthesis=True,
                metadata={
                    "source_file": filename,
                    "note": "PyMuPDF unavailable — PDF text extraction skipped",
                    **(metadata or {}),
                },
            )

        # PyMuPDF extraction path
        nodes = []
        text_parts = []
        try:
            doc = fitz.open(stream=content.encode("latin-1") if isinstance(content, str) else content,
                           filetype="pdf")

            # Document-level node
            doc_title = doc.metadata.get("title", "") or filename
            if doc_title:
                nodes.append({
                    "label": doc_title,
                    "kind": "document",
                    "summary": f"PDF document ({doc.page_count} pages)",
                    "properties": {
                        "source_file": filename,
                        "page_count": doc.page_count,
                        "author": doc.metadata.get("author", ""),
                    },
                })

            for page in doc:
                text = page.get_text()
                if text.strip():
                    text_parts.append(text)

            doc.close()
        except Exception as e:
            return ExtractionResult(
                source_type="pdf",
                content_type="application/pdf",
                raw_content=content if isinstance(content, str) else "",
                needs_synthesis=True,
                metadata={
                    "source_file": filename,
                    "error": f"PDF parsing failed: {e}",
                    **(metadata or {}),
                },
            )

        full_text = "\n".join(text_parts)
        return ExtractionResult(
            source_type="pdf",
            content_type="application/pdf",
            nodes=nodes,
            raw_content=full_text,
            needs_synthesis=len(full_text) > 200,
            metadata={"source_file": filename, **(metadata or {})},
        )
```

- [ ] **Step 3: Run tests, commit**

---

## Task 4: Register New Extractors in Pipeline

**Files:**
- Modify: `images/knowledge/ingestion/pipeline.py`
- Test: `images/tests/test_ingestion_pipeline.py` (append)

- [ ] **Step 1: Write failing test**

Append to `images/tests/test_ingestion_pipeline.py`:

```python
class TestPipelineNewExtractors:
    def test_ingest_html(self, tmp_path):
        from store import KnowledgeStore
        from ingestion.pipeline import IngestionPipeline

        store = KnowledgeStore(tmp_path)
        pipeline = IngestionPipeline(store=store)
        html = "<html><head><title>Test Page</title></head><body><h1>Overview</h1><p>Content</p></body></html>"
        stats = pipeline.ingest(html, filename="page.html")
        assert stats["nodes_created"] > 0
        assert stats["extractor"] == "html"

    def test_ingest_python(self, tmp_path):
        from store import KnowledgeStore
        from ingestion.pipeline import IngestionPipeline

        store = KnowledgeStore(tmp_path)
        pipeline = IngestionPipeline(store=store)
        code = "def authenticate(user, password):\n    return check(user, password)\n"
        stats = pipeline.ingest(code, filename="auth.py")
        assert stats["nodes_created"] > 0
        assert stats["extractor"] == "code"
```

- [ ] **Step 2: Update pipeline to register new extractors**

In `images/knowledge/ingestion/pipeline.py`, update `_load_extractors()`:

```python
    def _load_extractors(self):
        from ingestion.extractors.config import ConfigExtractor
        from ingestion.extractors.html_extractor import HtmlExtractor
        from ingestion.extractors.code import CodeExtractor
        from ingestion.extractors.markdown import MarkdownExtractor
        from ingestion.extractors.structured import StructuredExtractor

        self._extractors = [
            ConfigExtractor(),      # YAML/JSON/TOML
            HtmlExtractor(),        # HTML pages
            CodeExtractor(),        # Source code
            MarkdownExtractor(),    # Markdown/text
            StructuredExtractor(),  # Fallback — any text
        ]

        # Optional: PDF (requires PyMuPDF)
        try:
            from ingestion.extractors.pdf import PdfExtractor
            pdf = PdfExtractor()
            if pdf.available:
                self._extractors.insert(0, pdf)  # Before config (more specific)
        except ImportError:
            pass
```

- [ ] **Step 3: Run tests, commit**

---

## Task 5: Watch Mode

**Files:**
- Create: `images/knowledge/ingestion/watcher.py`
- Test: `images/tests/test_watcher.py`
- Modify: `images/knowledge/server.py`

- [ ] **Step 1: Write failing tests**

```python
# images/tests/test_watcher.py
"""Tests for directory watch mode."""
import os
import sys
import time
import tempfile
import pytest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "knowledge"))

from ingestion.watcher import FileWatcher


class TestFileWatcherAvailability:
    def test_reports_availability(self):
        """Watcher should report whether watchdog is available."""
        assert isinstance(FileWatcher.available(), bool)


class TestFileWatcherCallbacks:
    def test_on_file_created_callback(self, tmp_path):
        """Watcher should call the callback when a file is created."""
        if not FileWatcher.available():
            pytest.skip("watchdog not installed")

        received = []
        def on_file(path):
            received.append(path)

        watcher = FileWatcher(str(tmp_path), on_file=on_file)
        watcher.start()
        try:
            # Create a file
            test_file = tmp_path / "test.md"
            test_file.write_text("# Test\nContent here")
            time.sleep(0.5)  # Give watcher time to detect
        finally:
            watcher.stop()

        assert len(received) >= 1
        assert str(test_file) in received[0]


class TestFileWatcherFiltering:
    def test_ignores_dotfiles(self, tmp_path):
        """Watcher should ignore hidden files."""
        if not FileWatcher.available():
            pytest.skip("watchdog not installed")

        received = []
        watcher = FileWatcher(str(tmp_path), on_file=lambda p: received.append(p))
        watcher.start()
        try:
            (tmp_path / ".hidden").write_text("secret")
            time.sleep(0.5)
        finally:
            watcher.stop()

        assert len(received) == 0


class TestFileWatcherWithoutWatchdog:
    def test_polling_fallback_exists(self):
        """Without watchdog, a polling fallback should be available."""
        # The watcher should have a poll_once() method for manual triggering
        watcher = FileWatcher("/tmp/nonexistent", on_file=lambda p: None)
        assert hasattr(watcher, "poll_once")
```

- [ ] **Step 2: Implement FileWatcher**

```python
# images/knowledge/ingestion/watcher.py
"""Directory watch mode for auto-ingesting files.

Uses watchdog if available for efficient filesystem events.
Falls back to periodic polling if watchdog isn't installed.
"""
import os
import logging
import time
from pathlib import Path
from typing import Callable, Optional

logger = logging.getLogger(__name__)

try:
    from watchdog.observers import Observer
    from watchdog.events import FileSystemEventHandler, FileCreatedEvent, FileModifiedEvent
    _WATCHDOG_AVAILABLE = True
except ImportError:
    _WATCHDOG_AVAILABLE = False

# File extensions to ingest
_INGEST_EXTENSIONS = {
    ".md", ".txt", ".yaml", ".yml", ".json", ".toml",
    ".py", ".go", ".js", ".ts", ".java", ".rs", ".rb",
    ".html", ".htm", ".pdf", ".csv",
}


class FileWatcher:
    """Watches a directory and calls on_file for new/modified files."""

    def __init__(self, watch_dir: str, on_file: Callable[[str], None],
                 extensions: Optional[set] = None):
        self.watch_dir = watch_dir
        self.on_file = on_file
        self.extensions = extensions or _INGEST_EXTENSIONS
        self._observer = None
        self._seen = set()  # Track processed files for polling mode

    @staticmethod
    def available() -> bool:
        return _WATCHDOG_AVAILABLE

    def start(self):
        """Start watching. Uses watchdog if available."""
        os.makedirs(self.watch_dir, exist_ok=True)
        if _WATCHDOG_AVAILABLE:
            self._start_watchdog()
        else:
            logger.info("watchdog not available — use poll_once() for manual polling")

    def stop(self):
        if self._observer:
            self._observer.stop()
            self._observer.join(timeout=5)
            self._observer = None

    def poll_once(self) -> list:
        """Manually poll for new files. Works with or without watchdog."""
        results = []
        watch_path = Path(self.watch_dir)
        if not watch_path.exists():
            return results
        for f in watch_path.iterdir():
            if f.is_file() and not f.name.startswith(".") and f.suffix in self.extensions:
                fpath = str(f)
                mtime = f.stat().st_mtime
                key = (fpath, mtime)
                if key not in self._seen:
                    self._seen.add(key)
                    self.on_file(fpath)
                    results.append(fpath)
        return results

    def _start_watchdog(self):
        handler = _IngestHandler(self.on_file, self.extensions)
        self._observer = Observer()
        self._observer.schedule(handler, self.watch_dir, recursive=False)
        self._observer.daemon = True
        self._observer.start()


if _WATCHDOG_AVAILABLE:
    class _IngestHandler(FileSystemEventHandler):
        def __init__(self, on_file, extensions):
            self.on_file = on_file
            self.extensions = extensions

        def on_created(self, event):
            if not event.is_directory:
                self._handle(event.src_path)

        def on_modified(self, event):
            if not event.is_directory:
                self._handle(event.src_path)

        def _handle(self, path):
            name = os.path.basename(path)
            if name.startswith("."):
                return
            _, ext = os.path.splitext(name)
            if ext not in self.extensions:
                return
            self.on_file(path)
else:
    class _IngestHandler:
        """Stub when watchdog unavailable."""
        pass
```

- [ ] **Step 3: Wire watcher into server.py**

In `images/knowledge/server.py`, add optional watcher lifecycle:

```python
# In create_app(), after pipeline init:
watch_dir = os.environ.get("KNOWLEDGE_WATCH_DIR", "")
if watch_dir:
    from ingestion.watcher import FileWatcher

    def _on_file(path):
        with open(path, "r") as f:
            content = f.read()
        pipeline.ingest(content, filename=os.path.basename(path))

    watcher = FileWatcher(watch_dir, on_file=_on_file)
    app["watcher"] = watcher
    app.on_startup.append(lambda app: app["watcher"].start())
    app.on_cleanup.append(lambda app: app["watcher"].stop())
```

- [ ] **Step 4: Run tests, commit**

---

## Task 6: Update requirements.txt with Optional Dependencies

**Files:**
- Modify: `images/knowledge/requirements.txt`

- [ ] **Step 1: Add optional deps as comments**

```
# images/knowledge/requirements.txt
aiohttp>=3.9
httpx>=0.27
pyyaml>=6.0
sqlite-vec>=0.1.0

# Optional — install for enhanced extraction:
# beautifulsoup4>=4.12    # Better HTML parsing
# tree-sitter>=0.22       # AST-based code extraction
# pymupdf>=1.24           # PDF text extraction
# watchdog>=4.0           # Directory watch mode
```

Don't add them as hard dependencies — all extractors degrade gracefully without them.

- [ ] **Step 2: Commit**

---

## Task 7: Full Test Suite Validation

- [ ] **Step 1: Run all Phase 2b tests**

```bash
python3 -m pytest images/tests/test_html_extractor.py images/tests/test_code_extractor.py images/tests/test_pdf_extractor.py images/tests/test_watcher.py -v
```

- [ ] **Step 2: Run Phase 2 + Phase 1 tests for regressions**

```bash
python3 -m pytest images/tests/test_extractors.py images/tests/test_source_classifier.py images/tests/test_merge_buffer.py images/tests/test_ingestion_pipeline.py images/tests/test_edge_provenance.py images/tests/test_principal_registry.py images/tests/test_scope_model.py -v
```

- [ ] **Step 3: Build Go gateway**

```bash
go build ./cmd/gateway/
```

- [ ] **Step 4: Commit any fixes**

---

## Summary

Phase 2b delivers extended extractors and watch mode:

| Component | What it adds |
|-----------|-------------|
| **HtmlExtractor** | Title, headings, links, meta from HTML via stdlib parser |
| **CodeExtractor** | Functions, classes, structs, imports via regex for Python/Go/JS/TS |
| **PdfExtractor** | PyMuPDF text extraction with graceful fallback |
| **FileWatcher** | Directory watch with watchdog or polling fallback |
| **Pipeline update** | Auto-discovers and registers new extractors |

All extractors degrade gracefully without optional deps — no hard requirements added.

**Next:** Phase 3 (Graph Intelligence — Leiden community detection, hub analysis)
