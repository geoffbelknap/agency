# Knowledge Graph Intelligence — Phase 2: Universal Ingestion Pipeline

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend the knowledge service to ingest from any source type — not just comms messages. Build a dual extraction pipeline where deterministic parsers handle structured content (zero tokens) and the LLM synthesizer only fires for semantic gaps.

**Architecture:** Content flows through SourceClassifier → DeterministicExtractor(s) → MergeBuffer → optional LLMSynthesizer → KnowledgeStore. Each extractor produces `{nodes, edges}` with provenance tags. A new `POST /ingest` endpoint accepts raw content. The `agency knowledge ingest` CLI command enables operator-initiated ingestion.

**Tech Stack:** Python (knowledge service), Go (gateway/CLI), aiohttp, tree-sitter (code), PyMuPDF (PDF), BeautifulSoup (HTML)

**Spec:** `docs/specs/knowledge-graph-intelligence.md` — Phase 2 section

**Depends on:** Phase 1 (edge provenance, scope model) — completed

---

## File Structure

### Files to Create

| File | Purpose |
|------|---------|
| `images/knowledge/ingestion/__init__.py` | Package init |
| `images/knowledge/ingestion/classifier.py` | SourceClassifier — routes content to extractors by type |
| `images/knowledge/ingestion/base.py` | ExtractionResult dataclass and BaseExtractor ABC |
| `images/knowledge/ingestion/merge_buffer.py` | MergeBuffer — decides if LLM synthesis adds value |
| `images/knowledge/ingestion/pipeline.py` | IngestionPipeline — orchestrates classify → extract → merge → synthesize → store |
| `images/knowledge/ingestion/extractors/__init__.py` | Package init |
| `images/knowledge/ingestion/extractors/markdown.py` | Markdown/text deterministic extractor |
| `images/knowledge/ingestion/extractors/config.py` | Config file (YAML/JSON/TOML) deterministic extractor |
| `images/knowledge/ingestion/extractors/code.py` | Code file extractor (tree-sitter AST) |
| `images/knowledge/ingestion/extractors/html.py` | HTML/web page extractor |
| `images/knowledge/ingestion/extractors/structured.py` | Tool output / structured data extractor |
| `images/tests/test_source_classifier.py` | Tests for classifier |
| `images/tests/test_extractors.py` | Tests for all deterministic extractors |
| `images/tests/test_merge_buffer.py` | Tests for merge buffer |
| `images/tests/test_ingestion_pipeline.py` | Integration tests for full pipeline |

### Files to Modify

| File | Changes |
|------|---------|
| `images/knowledge/server.py` | Add `POST /ingest` universal endpoint, wire pipeline |
| `images/knowledge/synthesizer.py` | Extend to accept raw content (not just comms messages) |
| `internal/knowledge/proxy.go` | Add Ingest proxy method |
| `internal/api/routes.go` | Add ingest route |
| `internal/api/handlers_hub.go` | Add ingest handler |
| `internal/apiclient/client.go` | Add KnowledgeIngest client method |
| `internal/cli/commands.go` | Add `agency knowledge ingest` subcommand |

---

## Task 1: ExtractionResult and BaseExtractor

**Files:**
- Create: `images/knowledge/ingestion/__init__.py`
- Create: `images/knowledge/ingestion/base.py`
- Test: `images/tests/test_extractors.py`

- [ ] **Step 1: Write the failing test**

```python
# images/tests/test_extractors.py
"""Tests for deterministic extractors."""
import os
import sys
import pytest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "knowledge"))

from ingestion.base import ExtractionResult, BaseExtractor


class TestExtractionResult:
    def test_empty_result(self):
        r = ExtractionResult(source_type="test", content_type="text/plain")
        assert r.nodes == []
        assert r.edges == []
        assert r.raw_content == ""
        assert r.needs_synthesis is True

    def test_with_nodes_and_edges(self):
        r = ExtractionResult(
            source_type="markdown",
            content_type="text/markdown",
            nodes=[{"label": "test", "kind": "concept", "summary": "a test"}],
            edges=[{"source_label": "a", "target_label": "b", "relation": "relates_to"}],
            raw_content="# Test\nSome content",
            needs_synthesis=False,
        )
        assert len(r.nodes) == 1
        assert len(r.edges) == 1
        assert r.needs_synthesis is False

    def test_node_provenance_default(self):
        r = ExtractionResult(source_type="config", content_type="application/yaml")
        r.nodes.append({"label": "test", "kind": "configuration"})
        # ExtractionResult should provide a default provenance for its nodes
        assert r.default_provenance == "EXTRACTED"

    def test_merge_results(self):
        a = ExtractionResult(source_type="markdown", content_type="text/markdown",
                             nodes=[{"label": "A"}], edges=[])
        b = ExtractionResult(source_type="markdown", content_type="text/markdown",
                             nodes=[{"label": "B"}], edges=[{"source_label": "A", "target_label": "B", "relation": "r"}])
        merged = a.merge(b)
        assert len(merged.nodes) == 2
        assert len(merged.edges) == 1
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/geoff/agency-workspace/agency && python3 -m pytest images/tests/test_extractors.py::TestExtractionResult -v`

- [ ] **Step 3: Implement base module**

```python
# images/knowledge/ingestion/__init__.py
"""Universal ingestion pipeline for the knowledge graph."""

# images/knowledge/ingestion/base.py
"""Base types for the ingestion pipeline."""
from abc import ABC, abstractmethod
from dataclasses import dataclass, field
from typing import Optional


@dataclass
class ExtractionResult:
    """Output of a deterministic extractor."""
    source_type: str          # e.g., "markdown", "config", "code"
    content_type: str         # MIME type: "text/markdown", "application/yaml", etc.
    nodes: list = field(default_factory=list)
    edges: list = field(default_factory=list)
    raw_content: str = ""     # Original content for LLM synthesis if needed
    needs_synthesis: bool = True  # Should MergeBuffer send to LLM?
    default_provenance: str = "EXTRACTED"
    metadata: dict = field(default_factory=dict)

    def merge(self, other: "ExtractionResult") -> "ExtractionResult":
        """Merge two extraction results."""
        return ExtractionResult(
            source_type=self.source_type,
            content_type=self.content_type,
            nodes=self.nodes + other.nodes,
            edges=self.edges + other.edges,
            raw_content=self.raw_content or other.raw_content,
            needs_synthesis=self.needs_synthesis or other.needs_synthesis,
            default_provenance=self.default_provenance,
            metadata={**self.metadata, **other.metadata},
        )


class BaseExtractor(ABC):
    """Abstract base for deterministic content extractors."""

    @abstractmethod
    def can_handle(self, content_type: str, filename: str = "") -> bool:
        """Return True if this extractor handles the given content type."""

    @abstractmethod
    def extract(self, content: str, filename: str = "",
                metadata: Optional[dict] = None) -> ExtractionResult:
        """Extract nodes and edges from content."""

    @property
    @abstractmethod
    def name(self) -> str:
        """Extractor name for logging."""
```

- [ ] **Step 4: Create package init**

```python
# images/knowledge/ingestion/extractors/__init__.py
"""Deterministic content extractors."""
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd /home/geoff/agency-workspace/agency && python3 -m pytest images/tests/test_extractors.py::TestExtractionResult -v`

- [ ] **Step 6: Commit**

```bash
git add images/knowledge/ingestion/ images/tests/test_extractors.py
git commit -m "feat(knowledge): add ExtractionResult and BaseExtractor for ingestion pipeline"
```

---

## Task 2: SourceClassifier

**Files:**
- Create: `images/knowledge/ingestion/classifier.py`
- Test: `images/tests/test_source_classifier.py`

- [ ] **Step 1: Write the failing tests**

```python
# images/tests/test_source_classifier.py
"""Tests for SourceClassifier."""
import os
import sys
import pytest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "knowledge"))

from ingestion.classifier import SourceClassifier


class TestSourceClassifier:
    def test_classify_markdown_by_extension(self):
        assert SourceClassifier.classify(filename="README.md") == "text/markdown"

    def test_classify_yaml_by_extension(self):
        assert SourceClassifier.classify(filename="config.yaml") == "application/yaml"

    def test_classify_json_by_extension(self):
        assert SourceClassifier.classify(filename="data.json") == "application/json"

    def test_classify_python_by_extension(self):
        assert SourceClassifier.classify(filename="app.py") == "text/x-python"

    def test_classify_go_by_extension(self):
        assert SourceClassifier.classify(filename="main.go") == "text/x-go"

    def test_classify_html_by_extension(self):
        assert SourceClassifier.classify(filename="page.html") == "text/html"

    def test_classify_toml_by_extension(self):
        assert SourceClassifier.classify(filename="pyproject.toml") == "application/toml"

    def test_classify_by_explicit_content_type(self):
        assert SourceClassifier.classify(content_type="text/markdown") == "text/markdown"

    def test_classify_by_content_sniffing_yaml(self):
        content = "---\nname: test\nversion: 1\n"
        assert SourceClassifier.classify(content=content) == "application/yaml"

    def test_classify_by_content_sniffing_json(self):
        content = '{"key": "value"}'
        assert SourceClassifier.classify(content=content) == "application/json"

    def test_classify_by_content_sniffing_html(self):
        content = "<!DOCTYPE html><html><body>Hello</body></html>"
        assert SourceClassifier.classify(content=content) == "text/html"

    def test_classify_unknown_defaults_to_text(self):
        assert SourceClassifier.classify(filename="mystery.xyz") == "text/plain"

    def test_classify_url(self):
        assert SourceClassifier.classify(filename="https://example.com/page") == "text/html"

    def test_is_code(self):
        assert SourceClassifier.is_code("text/x-python")
        assert SourceClassifier.is_code("text/x-go")
        assert not SourceClassifier.is_code("text/markdown")

    def test_is_config(self):
        assert SourceClassifier.is_config("application/yaml")
        assert SourceClassifier.is_config("application/json")
        assert SourceClassifier.is_config("application/toml")
        assert not SourceClassifier.is_config("text/markdown")
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/geoff/agency-workspace/agency && python3 -m pytest images/tests/test_source_classifier.py -v`

- [ ] **Step 3: Implement SourceClassifier**

```python
# images/knowledge/ingestion/classifier.py
"""Classifies content by type for routing to the correct extractor."""
import os
from typing import Optional

# Extension to MIME type mapping
_EXT_MAP = {
    # Markdown/text
    ".md": "text/markdown",
    ".markdown": "text/markdown",
    ".txt": "text/plain",
    ".rst": "text/plain",
    # Config
    ".yaml": "application/yaml",
    ".yml": "application/yaml",
    ".json": "application/json",
    ".toml": "application/toml",
    ".ini": "text/plain",
    ".env": "text/plain",
    # Code
    ".py": "text/x-python",
    ".go": "text/x-go",
    ".js": "text/javascript",
    ".ts": "text/typescript",
    ".jsx": "text/javascript",
    ".tsx": "text/typescript",
    ".java": "text/x-java",
    ".rs": "text/x-rust",
    ".rb": "text/x-ruby",
    ".c": "text/x-c",
    ".cpp": "text/x-c++",
    ".h": "text/x-c",
    ".cs": "text/x-csharp",
    ".swift": "text/x-swift",
    ".kt": "text/x-kotlin",
    ".sh": "text/x-shellscript",
    ".bash": "text/x-shellscript",
    # HTML
    ".html": "text/html",
    ".htm": "text/html",
    # PDF
    ".pdf": "application/pdf",
    # Images
    ".png": "image/png",
    ".jpg": "image/jpeg",
    ".jpeg": "image/jpeg",
    ".gif": "image/gif",
    ".svg": "image/svg+xml",
}

_CODE_TYPES = {v for k, v in _EXT_MAP.items() if v.startswith("text/x-") or v in ("text/javascript", "text/typescript")}
_CONFIG_TYPES = {"application/yaml", "application/json", "application/toml"}


class SourceClassifier:
    """Classifies content by MIME type for extractor routing."""

    @staticmethod
    def classify(
        filename: str = "",
        content_type: str = "",
        content: str = "",
    ) -> str:
        """Determine content type from filename, explicit type, or content sniffing."""
        # Explicit type wins
        if content_type:
            return content_type

        # URL detection
        if filename.startswith(("http://", "https://")):
            return "text/html"

        # Extension-based
        if filename:
            _, ext = os.path.splitext(filename.lower())
            if ext in _EXT_MAP:
                return _EXT_MAP[ext]

        # Content sniffing
        if content:
            stripped = content.strip()
            if stripped.startswith(("<!DOCTYPE", "<html", "<!doctype")):
                return "text/html"
            if stripped.startswith("{") or stripped.startswith("["):
                return "application/json"
            if stripped.startswith("---") or ": " in stripped.split("\n")[0]:
                return "application/yaml"

        return "text/plain"

    @staticmethod
    def is_code(content_type: str) -> bool:
        return content_type in _CODE_TYPES

    @staticmethod
    def is_config(content_type: str) -> bool:
        return content_type in _CONFIG_TYPES

    @staticmethod
    def is_image(content_type: str) -> bool:
        return content_type.startswith("image/")
```

- [ ] **Step 4: Run test to verify it passes**

- [ ] **Step 5: Commit**

```bash
git add images/knowledge/ingestion/classifier.py images/tests/test_source_classifier.py
git commit -m "feat(knowledge): add SourceClassifier for content type routing"
```

---

## Task 3: Markdown Extractor

**Files:**
- Create: `images/knowledge/ingestion/extractors/markdown.py`
- Test: `images/tests/test_extractors.py` (append)

- [ ] **Step 1: Write failing tests**

Append to `images/tests/test_extractors.py`:

```python
from ingestion.extractors.markdown import MarkdownExtractor


class TestMarkdownExtractor:
    @pytest.fixture
    def extractor(self):
        return MarkdownExtractor()

    def test_can_handle_markdown(self, extractor):
        assert extractor.can_handle("text/markdown")
        assert extractor.can_handle("text/plain")
        assert not extractor.can_handle("application/json")

    def test_extract_headings_as_concepts(self, extractor):
        content = "# Architecture\n\nThe system uses microservices.\n\n## Components\n\nThere are three main components."
        result = extractor.extract(content, filename="design.md")
        labels = [n["label"] for n in result.nodes]
        assert "Architecture" in labels
        assert "Components" in labels

    def test_extract_links_as_edges(self, extractor):
        content = "# Auth\n\nSee [Database](database.md) for schema.\n\n# Database\n\nStores user data."
        result = extractor.extract(content, filename="overview.md")
        # Should have edges from cross-references
        assert len(result.edges) > 0

    def test_extract_urls(self, extractor):
        content = "# Setup\n\nVisit https://example.com/docs for details."
        result = extractor.extract(content, filename="setup.md")
        url_nodes = [n for n in result.nodes if n.get("kind") == "url"]
        assert len(url_nodes) >= 1

    def test_needs_synthesis_for_prose(self, extractor):
        content = "# Analysis\n\nThe vulnerability in nginx allows remote code execution through crafted HTTP/2 requests."
        result = extractor.extract(content, filename="analysis.md")
        assert result.needs_synthesis is True

    def test_config_only_no_synthesis(self, extractor):
        """Short structural documents shouldn't need synthesis."""
        content = "# Config\n\n- port: 8080\n- host: localhost"
        result = extractor.extract(content, filename="config.md")
        # Short, list-based content — deterministic extraction sufficient
        # (needs_synthesis depends on content length/complexity)

    def test_source_file_in_metadata(self, extractor):
        result = extractor.extract("# Test", filename="test.md")
        assert result.metadata.get("source_file") == "test.md"
```

- [ ] **Step 2: Implement MarkdownExtractor**

```python
# images/knowledge/ingestion/extractors/markdown.py
"""Deterministic extractor for markdown and plain text documents."""
import re
from typing import Optional

from ingestion.base import ExtractionResult, BaseExtractor

# Regex patterns
_HEADING_RE = re.compile(r"^(#{1,6})\s+(.+)$", re.MULTILINE)
_LINK_RE = re.compile(r"\[([^\]]+)\]\(([^)]+)\)")
_URL_RE = re.compile(r"https?://[^\s<>\"')\]]+")
_SYNTHESIS_THRESHOLD = 200  # chars of prose content triggers synthesis


class MarkdownExtractor(BaseExtractor):
    """Extracts structure from markdown: headings, links, URLs, cross-references."""

    @property
    def name(self) -> str:
        return "markdown"

    def can_handle(self, content_type: str, filename: str = "") -> bool:
        return content_type in ("text/markdown", "text/plain")

    def extract(self, content: str, filename: str = "",
                metadata: Optional[dict] = None) -> ExtractionResult:
        nodes = []
        edges = []
        headings = []

        # Extract headings as concept nodes
        for match in _HEADING_RE.finditer(content):
            level = len(match.group(1))
            title = match.group(2).strip()
            if title:
                headings.append({"label": title, "level": level})
                nodes.append({
                    "label": title,
                    "kind": "concept",
                    "summary": f"Section heading (level {level}) from {filename or 'document'}",
                    "properties": {"heading_level": level, "source_file": filename},
                })

        # Create parent-child edges between headings
        for i, h in enumerate(headings):
            for j in range(i - 1, -1, -1):
                if headings[j]["level"] < h["level"]:
                    edges.append({
                        "source_label": h["label"],
                        "target_label": headings[j]["label"],
                        "relation": "part_of",
                    })
                    break

        # Extract markdown links
        for match in _LINK_RE.finditer(content):
            link_text, link_target = match.group(1), match.group(2)
            if link_target.startswith(("http://", "https://")):
                nodes.append({
                    "label": link_target,
                    "kind": "url",
                    "summary": link_text,
                    "properties": {"source_file": filename},
                })
            elif link_target.endswith(".md"):
                # Cross-reference to another document
                ref_name = link_target.replace(".md", "").replace("-", " ").replace("_", " ").title()
                edges.append({
                    "source_label": headings[0]["label"] if headings else filename,
                    "target_label": ref_name,
                    "relation": "relates_to",
                })

        # Extract bare URLs
        for match in _URL_RE.finditer(content):
            url = match.group(0)
            if not any(n.get("label") == url for n in nodes):
                nodes.append({
                    "label": url,
                    "kind": "url",
                    "summary": f"URL referenced in {filename or 'document'}",
                    "properties": {"source_file": filename},
                })

        # Determine if LLM synthesis adds value
        # Strip headings and formatting, measure remaining prose
        prose = _HEADING_RE.sub("", content)
        prose = _LINK_RE.sub(r"\1", prose)
        prose_len = len(prose.strip())
        needs_synthesis = prose_len > _SYNTHESIS_THRESHOLD

        return ExtractionResult(
            source_type="markdown",
            content_type="text/markdown",
            nodes=nodes,
            edges=edges,
            raw_content=content,
            needs_synthesis=needs_synthesis,
            metadata={"source_file": filename, **(metadata or {})},
        )
```

- [ ] **Step 3: Run tests**

- [ ] **Step 4: Commit**

---

## Task 4: Config File Extractor

**Files:**
- Create: `images/knowledge/ingestion/extractors/config.py`
- Test: `images/tests/test_extractors.py` (append)

- [ ] **Step 1: Write failing tests**

Append to `images/tests/test_extractors.py`:

```python
from ingestion.extractors.config import ConfigExtractor


class TestConfigExtractor:
    @pytest.fixture
    def extractor(self):
        return ConfigExtractor()

    def test_can_handle_yaml(self, extractor):
        assert extractor.can_handle("application/yaml")

    def test_can_handle_json(self, extractor):
        assert extractor.can_handle("application/json")

    def test_can_handle_toml(self, extractor):
        assert extractor.can_handle("application/toml")

    def test_extract_yaml_keys(self, extractor):
        content = "database:\n  host: localhost\n  port: 5432\nredis:\n  host: cache.internal"
        result = extractor.extract(content, filename="config.yaml")
        labels = [n["label"] for n in result.nodes]
        assert any("database" in l.lower() for l in labels)

    def test_extract_json_keys(self, extractor):
        content = '{"api": {"endpoint": "https://api.example.com", "timeout": 30}}'
        result = extractor.extract(content, filename="settings.json")
        assert len(result.nodes) > 0

    def test_no_synthesis_needed(self, extractor):
        """Config files are fully captured by deterministic extraction."""
        content = "port: 8080\nhost: 0.0.0.0"
        result = extractor.extract(content, filename="simple.yaml")
        assert result.needs_synthesis is False

    def test_extracts_urls_from_values(self, extractor):
        content = "api_url: https://api.example.com/v1\nwebhook: https://hooks.example.com"
        result = extractor.extract(content, filename="endpoints.yaml")
        url_nodes = [n for n in result.nodes if n.get("kind") == "url"]
        assert len(url_nodes) >= 1
```

- [ ] **Step 2: Implement ConfigExtractor**

Parses YAML/JSON/TOML, creates `config_item` nodes for significant key-value pairs, extracts URLs from values, creates structural edges between nested keys. Sets `needs_synthesis=False` — config files are fully captured deterministically.

- [ ] **Step 3: Run tests, commit**

---

## Task 5: Structured Data Extractor (Tool Outputs)

**Files:**
- Create: `images/knowledge/ingestion/extractors/structured.py`
- Test: `images/tests/test_extractors.py` (append)

- [ ] **Step 1: Write failing tests**

Append to `images/tests/test_extractors.py`:

```python
from ingestion.extractors.structured import StructuredExtractor


class TestStructuredExtractor:
    @pytest.fixture
    def extractor(self):
        return StructuredExtractor()

    def test_extracts_ips(self, extractor):
        content = "Found connections from 192.168.1.100 and 10.0.0.1 to external 8.8.8.8"
        result = extractor.extract(content, filename="scan_output.txt")
        labels = [n["label"] for n in result.nodes]
        assert "192.168.1.100" in labels

    def test_extracts_cves(self, extractor):
        content = "Vulnerability CVE-2023-44487 affects the HTTP/2 implementation"
        result = extractor.extract(content, filename="vuln_report.txt")
        cve_nodes = [n for n in result.nodes if "CVE" in n["label"]]
        assert len(cve_nodes) >= 1

    def test_extracts_urls(self, extractor):
        content = "Download from https://example.com/release/v1.0.tar.gz"
        result = extractor.extract(content, filename="notes.txt")
        url_nodes = [n for n in result.nodes if n.get("kind") == "url"]
        assert len(url_nodes) >= 1

    def test_extracts_error_codes(self, extractor):
        content = "Request failed with HTTP 503 Service Unavailable"
        result = extractor.extract(content, filename="error.log")
        assert any("503" in str(n.get("properties", {})) for n in result.nodes)

    def test_needs_synthesis(self, extractor):
        """Tool outputs need synthesis for semantic relationships."""
        content = "The scan found 3 critical vulnerabilities in the production environment"
        result = extractor.extract(content)
        assert result.needs_synthesis is True
```

- [ ] **Step 2: Implement StructuredExtractor**

Regex-based extraction of IPs, CVEs, URLs, error codes, email addresses from text content. Always sets `needs_synthesis=True` since tool outputs need semantic analysis.

- [ ] **Step 3: Run tests, commit**

---

## Task 6: MergeBuffer

**Files:**
- Create: `images/knowledge/ingestion/merge_buffer.py`
- Test: `images/tests/test_merge_buffer.py`

- [ ] **Step 1: Write failing tests**

```python
# images/tests/test_merge_buffer.py
"""Tests for MergeBuffer — decides whether LLM synthesis adds value."""
import os
import sys
import pytest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "knowledge"))

from ingestion.base import ExtractionResult
from ingestion.merge_buffer import MergeBuffer


class TestMergeBuffer:
    def test_config_skips_synthesis(self):
        """Config extraction with needs_synthesis=False skips LLM."""
        result = ExtractionResult(
            source_type="config",
            content_type="application/yaml",
            nodes=[{"label": "db.host", "kind": "config_item"}],
            needs_synthesis=False,
        )
        decision = MergeBuffer.should_synthesize(result)
        assert decision is False

    def test_markdown_with_prose_triggers_synthesis(self):
        """Markdown with substantive prose triggers LLM synthesis."""
        result = ExtractionResult(
            source_type="markdown",
            content_type="text/markdown",
            nodes=[{"label": "Architecture", "kind": "concept"}],
            raw_content="# Architecture\n\n" + "The system uses a microservices architecture with event-driven communication. " * 10,
            needs_synthesis=True,
        )
        decision = MergeBuffer.should_synthesize(result)
        assert decision is True

    def test_empty_extraction_triggers_synthesis(self):
        """If deterministic extraction found nothing, try LLM."""
        result = ExtractionResult(
            source_type="text",
            content_type="text/plain",
            nodes=[],
            raw_content="This document describes the incident response procedure for production outages.",
            needs_synthesis=True,
        )
        decision = MergeBuffer.should_synthesize(result)
        assert decision is True

    def test_rich_extraction_skips_synthesis(self):
        """If deterministic extraction was thorough, skip LLM."""
        result = ExtractionResult(
            source_type="config",
            content_type="application/yaml",
            nodes=[{"label": f"key{i}", "kind": "config_item"} for i in range(10)],
            needs_synthesis=False,
        )
        decision = MergeBuffer.should_synthesize(result)
        assert decision is False

    def test_prepare_for_synthesis_returns_content(self):
        """prepare_for_synthesis returns content suitable for the LLM."""
        result = ExtractionResult(
            source_type="markdown",
            content_type="text/markdown",
            raw_content="# Test\nSome analysis of the system.",
            needs_synthesis=True,
        )
        prepared = MergeBuffer.prepare_for_synthesis(result)
        assert "Test" in prepared
        assert "analysis" in prepared
```

- [ ] **Step 2: Implement MergeBuffer**

```python
# images/knowledge/ingestion/merge_buffer.py
"""Decides whether LLM synthesis adds value after deterministic extraction."""

from ingestion.base import ExtractionResult


class MergeBuffer:
    """Gate between deterministic extraction and LLM synthesis.

    The decision is simple: if the extractor already set needs_synthesis=False
    (e.g., config files), skip. Otherwise, check if there's enough raw content
    to be worth synthesizing.
    """

    MIN_SYNTHESIS_CONTENT = 50  # chars minimum to bother with LLM

    @classmethod
    def should_synthesize(cls, result: ExtractionResult) -> bool:
        """Decide if this extraction result should go to the LLM."""
        if not result.needs_synthesis:
            return False
        if not result.raw_content or len(result.raw_content.strip()) < cls.MIN_SYNTHESIS_CONTENT:
            return False
        return True

    @classmethod
    def prepare_for_synthesis(cls, result: ExtractionResult) -> str:
        """Prepare content for LLM synthesis.

        Returns the raw content, potentially truncated for token efficiency.
        """
        content = result.raw_content.strip()
        # Cap at ~8000 chars (~2000 tokens) to stay within synthesis budgets
        if len(content) > 8000:
            content = content[:8000] + "\n\n[Content truncated]"
        return content
```

- [ ] **Step 3: Run tests, commit**

---

## Task 7: IngestionPipeline Orchestrator

**Files:**
- Create: `images/knowledge/ingestion/pipeline.py`
- Test: `images/tests/test_ingestion_pipeline.py`

- [ ] **Step 1: Write failing tests**

```python
# images/tests/test_ingestion_pipeline.py
"""Integration tests for the IngestionPipeline."""
import os
import sys
import pytest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "knowledge"))

from ingestion.pipeline import IngestionPipeline
from ingestion.base import ExtractionResult
from store import KnowledgeStore


class TestIngestionPipeline:
    @pytest.fixture
    def store(self, tmp_path):
        return KnowledgeStore(tmp_path)

    @pytest.fixture
    def pipeline(self, store):
        return IngestionPipeline(store=store, synthesizer=None)

    def test_ingest_config_file(self, pipeline, store):
        """Config file should produce nodes without LLM synthesis."""
        content = "database:\n  host: localhost\n  port: 5432"
        stats = pipeline.ingest(content, filename="db.yaml")
        assert stats["nodes_created"] > 0
        assert stats["synthesis_skipped"] is True

    def test_ingest_markdown_file(self, pipeline, store):
        """Markdown should produce heading nodes."""
        content = "# Security Policy\n\nAll access must be authenticated.\n\n## Exceptions\n\nNone."
        stats = pipeline.ingest(content, filename="policy.md")
        assert stats["nodes_created"] > 0

    def test_ingest_with_scope(self, pipeline, store):
        """Ingested content should carry the provided scope."""
        import json
        content = "port: 8080"
        scope = {"channels": ["ops"], "principals": ["operator:uuid-1"]}
        stats = pipeline.ingest(content, filename="config.yaml", scope=scope)
        # Verify nodes have the scope
        nodes = store._db.execute("SELECT scope FROM nodes WHERE scope != '{}'").fetchall()
        assert len(nodes) > 0
        stored_scope = json.loads(nodes[0][0])
        assert "ops" in stored_scope.get("channels", [])

    def test_ingest_with_explicit_content_type(self, pipeline):
        """Explicit content_type overrides filename-based classification."""
        content = "key: value"
        stats = pipeline.ingest(content, content_type="application/yaml")
        assert stats["nodes_created"] > 0

    def test_ingest_unknown_type_falls_back(self, pipeline):
        """Unknown content type should still attempt extraction."""
        content = "Some plain text about security vulnerabilities CVE-2023-12345"
        stats = pipeline.ingest(content, filename="notes.xyz")
        # Should at least try structured extraction
        assert stats["source_type"] is not None

    def test_ingest_returns_stats(self, pipeline):
        """Ingest should return statistics about what was processed."""
        stats = pipeline.ingest("# Test\nContent here", filename="test.md")
        assert "nodes_created" in stats
        assert "edges_created" in stats
        assert "source_type" in stats
        assert "synthesis_skipped" in stats or "synthesis_triggered" in stats
```

- [ ] **Step 2: Implement IngestionPipeline**

```python
# images/knowledge/ingestion/pipeline.py
"""Orchestrates the universal ingestion pipeline: classify → extract → merge → store."""
import json
import logging
from typing import Optional

from ingestion.classifier import SourceClassifier
from ingestion.base import ExtractionResult
from ingestion.merge_buffer import MergeBuffer

logger = logging.getLogger(__name__)


class IngestionPipeline:
    """Orchestrates content ingestion through the dual extraction pipeline."""

    def __init__(self, store, synthesizer=None):
        self.store = store
        self.synthesizer = synthesizer
        self._extractors = []
        self._load_extractors()

    def _load_extractors(self):
        """Load all available extractors."""
        from ingestion.extractors.markdown import MarkdownExtractor
        from ingestion.extractors.config import ConfigExtractor
        from ingestion.extractors.structured import StructuredExtractor

        self._extractors = [
            ConfigExtractor(),
            MarkdownExtractor(),
            StructuredExtractor(),  # Fallback — handles any text
        ]

    def ingest(
        self,
        content: str,
        filename: str = "",
        content_type: str = "",
        scope: Optional[dict] = None,
        source_principal: str = "",
    ) -> dict:
        """Ingest content through the dual extraction pipeline.

        Returns stats dict with nodes_created, edges_created, source_type, etc.
        """
        # 1. Classify
        detected_type = SourceClassifier.classify(
            filename=filename, content_type=content_type, content=content
        )

        # 2. Find matching extractor
        extractor = self._find_extractor(detected_type, filename)
        if not extractor:
            logger.warning("No extractor for content type %s", detected_type)
            return {"nodes_created": 0, "edges_created": 0, "source_type": detected_type,
                    "error": f"No extractor for {detected_type}"}

        # 3. Deterministic extraction
        result = extractor.extract(content, filename=filename)
        logger.info("Extractor %s produced %d nodes, %d edges",
                     extractor.name, len(result.nodes), len(result.edges))

        # 4. Store deterministic results
        nodes_created = 0
        edges_created = 0
        for node in result.nodes:
            self.store.add_node(
                label=node["label"],
                kind=node.get("kind", "fact"),
                summary=node.get("summary", ""),
                properties=node.get("properties", {}),
                source_type="rule",
                scope=scope,
            )
            nodes_created += 1

        for edge in result.edges:
            # Label-based resolution
            source_nodes = self.store.find_nodes(edge["source_label"], limit=1)
            target_nodes = self.store.find_nodes(edge["target_label"], limit=1)
            if source_nodes and target_nodes:
                self.store.add_edge(
                    source_id=source_nodes[0]["id"],
                    target_id=target_nodes[0]["id"],
                    relation=edge.get("relation", "relates_to"),
                    provenance=result.default_provenance,
                )
                edges_created += 1

        # 5. MergeBuffer decision
        synthesis_triggered = False
        synthesis_skipped = True
        if MergeBuffer.should_synthesize(result) and self.synthesizer:
            prepared = MergeBuffer.prepare_for_synthesis(result)
            # The synthesizer will handle its own batching
            self.synthesizer.add_content_for_synthesis(prepared, scope=scope)
            synthesis_triggered = True
            synthesis_skipped = False

        return {
            "nodes_created": nodes_created,
            "edges_created": edges_created,
            "source_type": detected_type,
            "extractor": extractor.name,
            "synthesis_triggered": synthesis_triggered,
            "synthesis_skipped": synthesis_skipped,
        }

    def _find_extractor(self, content_type, filename=""):
        """Find the first extractor that can handle this content type."""
        for ext in self._extractors:
            if ext.can_handle(content_type, filename):
                return ext
        return None
```

- [ ] **Step 3: Run tests, commit**

---

## Task 8: Extend LLMSynthesizer for Non-Comms Content

**Files:**
- Modify: `images/knowledge/synthesizer.py`
- Test: `images/tests/test_ingestion_pipeline.py` (append)

- [ ] **Step 1: Write failing test**

Append to `images/tests/test_ingestion_pipeline.py`:

```python
class TestSynthesizerContentExtension:
    def test_add_content_for_synthesis(self):
        """Synthesizer should accept raw content (not just comms messages)."""
        from synthesizer import LLMSynthesizer
        from store import KnowledgeStore

        store = KnowledgeStore(":memory:")
        synth = LLMSynthesizer(store)
        # Should not raise — method exists and accepts content
        synth.add_content_for_synthesis(
            "The nginx server on prod-web has a critical vulnerability",
            scope={"channels": ["security"]}
        )
        assert synth.has_pending_content()
```

- [ ] **Step 2: Add add_content_for_synthesis() to LLMSynthesizer**

In `images/knowledge/synthesizer.py`, add:
- `_pending_content: list` — stores raw content awaiting synthesis
- `add_content_for_synthesis(content, scope=None)` — queues content
- `has_pending_content() -> bool` — check if content is queued
- Update `should_synthesize()` to also check `_pending_content`
- Update `synthesize()` to handle both comms messages and raw content

The raw content path uses the same extraction prompt and `_apply_extraction()` — the only difference is the input format (raw text vs formatted comms messages).

- [ ] **Step 3: Run tests, commit**

---

## Task 9: POST /ingest Endpoint

**Files:**
- Modify: `images/knowledge/server.py`
- Test: `images/tests/test_ingestion_pipeline.py` (append)

- [ ] **Step 1: Write failing test**

Append to `images/tests/test_ingestion_pipeline.py`:

```python
class TestIngestEndpoint:
    def test_ingest_endpoint_accepts_content(self):
        """POST /ingest should accept content with metadata."""
        # This is a contract test — verify the handler signature
        from server import handle_ingest_universal
        import inspect
        sig = inspect.signature(handle_ingest_universal)
        assert "request" in sig.parameters
```

- [ ] **Step 2: Add handle_ingest_universal to server.py**

In `images/knowledge/server.py`, add:

```python
async def handle_ingest_universal(request):
    """POST /ingest — universal content ingestion endpoint.

    Body: {
        "content": "...",          # Required: raw content
        "filename": "...",         # Optional: for type detection
        "content_type": "...",     # Optional: explicit MIME type
        "scope": {...},            # Optional: authorization scope
        "source_principal": "..."  # Optional: who provided this
    }
    """
    pipeline = request.app.get("pipeline")
    if not pipeline:
        return web.json_response({"error": "ingestion pipeline not initialized"}, status=503)

    body = await request.json()
    content = body.get("content", "")
    if not content:
        return web.json_response({"error": "content is required"}, status=400)

    stats = pipeline.ingest(
        content=content,
        filename=body.get("filename", ""),
        content_type=body.get("content_type", ""),
        scope=body.get("scope"),
        source_principal=body.get("source_principal", ""),
    )
    return web.json_response(stats)
```

Register route: `app.router.add_post("/ingest", handle_ingest_universal)`

Initialize pipeline in `create_app()`: `app["pipeline"] = IngestionPipeline(store=store, synthesizer=synth)`

- [ ] **Step 3: Verify server imports cleanly, commit**

---

## Task 10: Go Gateway Ingest Proxy + CLI

**Files:**
- Modify: `internal/knowledge/proxy.go`
- Modify: `internal/api/routes.go`
- Modify: `internal/api/handlers_hub.go`
- Modify: `internal/apiclient/client.go`
- Modify: `internal/cli/commands.go`

- [ ] **Step 1: Add proxy method**

In `internal/knowledge/proxy.go`:
```go
func (p *Proxy) Ingest(ctx context.Context, content, filename, contentType string, scope json.RawMessage) (json.RawMessage, error) {
    body := map[string]interface{}{
        "content":      content,
        "filename":     filename,
        "content_type": contentType,
    }
    if scope != nil {
        body["scope"] = scope
    }
    b, _ := json.Marshal(body)
    return p.post(ctx, "/ingest", b)
}
```

- [ ] **Step 2: Add route and handler**

Route: `r.Post("/knowledge/ingest", h.knowledgeIngest)`

Handler reads file content or accepts inline content, proxies to knowledge service.

- [ ] **Step 3: Add CLI command**

```
agency knowledge ingest <file-or-url> [--type content-type] [--scope '{"principals":["operator:uuid"]}']
```

For files: reads content from disk, detects filename. For URLs: passes the URL as filename (the knowledge service handles URL classification).

- [ ] **Step 4: Build, verify, commit**

---

## Task 11: Run Full Test Suite

- [ ] **Step 1: Run all new Phase 2 tests**

```bash
python3 -m pytest images/tests/test_source_classifier.py images/tests/test_extractors.py images/tests/test_merge_buffer.py images/tests/test_ingestion_pipeline.py -v
```

- [ ] **Step 2: Run Phase 1 tests for regressions**

```bash
python3 -m pytest images/tests/test_edge_provenance.py images/tests/test_principal_registry.py images/tests/test_scope_model.py -v
```

- [ ] **Step 3: Build Go gateway**

```bash
go build ./cmd/gateway/
```

- [ ] **Step 4: Commit any fixes**

---

## Summary

Phase 2 delivers the universal ingestion pipeline:

| Component | What it adds |
|-----------|-------------|
| **SourceClassifier** | Routes content by MIME type, extension, or sniffing |
| **BaseExtractor** | ABC for deterministic extractors with ExtractionResult |
| **MarkdownExtractor** | Headings → concepts, links → edges, URLs → url nodes |
| **ConfigExtractor** | YAML/JSON/TOML → config_item nodes, no synthesis needed |
| **StructuredExtractor** | IPs, CVEs, URLs, error codes from tool output text |
| **MergeBuffer** | Gates LLM synthesis — skips when deterministic extraction is sufficient |
| **IngestionPipeline** | Orchestrates classify → extract → merge → optional synthesis → store |
| **POST /ingest** | Universal ingestion endpoint on knowledge service |
| **CLI** | `agency knowledge ingest <file-or-url>` |

**Deferred to Phase 2b:**
- Tree-sitter code extractor (needs tree-sitter dependency in container)
- HTML/web page extractor (needs BeautifulSoup)
- PDF extractor (needs PyMuPDF)
- Watch mode for auto-ingestion directory
- Image ingestion via vision LLM

**Next:** Phase 3 (Graph Intelligence — Leiden community detection, hub analysis)
