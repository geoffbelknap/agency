"""Tests for ExtractionResult and BaseExtractor base types."""

import os
import sys
import pytest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "knowledge"))
from services.knowledge.ingestion.base import ExtractionResult, BaseExtractor


# ---------------------------------------------------------------------------
# ExtractionResult
# ---------------------------------------------------------------------------


class TestExtractionResultDefaults:
    """Empty / default ExtractionResult behaviour."""

    def test_empty_result_has_no_nodes(self):
        r = ExtractionResult(source_type="test", content_type="text/plain")
        assert r.nodes == []

    def test_empty_result_has_no_edges(self):
        r = ExtractionResult(source_type="test", content_type="text/plain")
        assert r.edges == []

    def test_empty_result_raw_content_is_empty(self):
        r = ExtractionResult(source_type="test", content_type="text/plain")
        assert r.raw_content == ""

    def test_empty_result_needs_synthesis_true(self):
        r = ExtractionResult(source_type="test", content_type="text/plain")
        assert r.needs_synthesis is True

    def test_default_provenance(self):
        r = ExtractionResult(source_type="test", content_type="text/plain")
        assert r.default_provenance == "EXTRACTED"

    def test_default_metadata_is_empty_dict(self):
        r = ExtractionResult(source_type="test", content_type="text/plain")
        assert r.metadata == {}

    def test_default_metadata_not_shared_between_instances(self):
        a = ExtractionResult(source_type="a", content_type="text/plain")
        b = ExtractionResult(source_type="b", content_type="text/plain")
        a.metadata["key"] = "val"
        assert "key" not in b.metadata


class TestExtractionResultWithData:
    """ExtractionResult populated with nodes and edges."""

    def _make_result(self):
        return ExtractionResult(
            source_type="markdown",
            content_type="text/markdown",
            nodes=[
                {"label": "Agency", "kind": "project", "summary": "AI agent platform", "properties": {}},
                {"label": "ASK", "kind": "framework", "summary": "Security tenets", "properties": {}},
            ],
            edges=[
                {"source_label": "Agency", "target_label": "ASK", "relation": "implements"},
            ],
            raw_content="# Agency\nImplements ASK.",
            needs_synthesis=False,
            default_provenance="MANUAL",
            metadata={"filename": "README.md"},
        )

    def test_node_count(self):
        r = self._make_result()
        assert len(r.nodes) == 2

    def test_edge_count(self):
        r = self._make_result()
        assert len(r.edges) == 1

    def test_source_type(self):
        r = self._make_result()
        assert r.source_type == "markdown"

    def test_content_type(self):
        r = self._make_result()
        assert r.content_type == "text/markdown"

    def test_custom_provenance(self):
        r = self._make_result()
        assert r.default_provenance == "MANUAL"

    def test_needs_synthesis_false(self):
        r = self._make_result()
        assert r.needs_synthesis is False

    def test_metadata_filename(self):
        r = self._make_result()
        assert r.metadata["filename"] == "README.md"


class TestExtractionResultMerge:
    """Merging two ExtractionResults."""

    def _left(self):
        return ExtractionResult(
            source_type="markdown",
            content_type="text/markdown",
            nodes=[{"label": "A", "kind": "entity", "summary": "", "properties": {}}],
            edges=[{"source_label": "A", "target_label": "B", "relation": "links"}],
            raw_content="left content",
            needs_synthesis=False,
            metadata={"origin": "left"},
        )

    def _right(self):
        return ExtractionResult(
            source_type="config",
            content_type="application/yaml",
            nodes=[{"label": "B", "kind": "entity", "summary": "", "properties": {}}],
            edges=[],
            raw_content="right content",
            needs_synthesis=True,
            metadata={"origin": "right"},
        )

    def test_merge_combines_nodes(self):
        merged = self._left().merge(self._right())
        assert len(merged.nodes) == 2

    def test_merge_combines_edges(self):
        merged = self._left().merge(self._right())
        assert len(merged.edges) == 1

    def test_merge_concatenates_raw_content(self):
        merged = self._left().merge(self._right())
        assert "left content" in merged.raw_content
        assert "right content" in merged.raw_content

    def test_merge_needs_synthesis_if_either_does(self):
        merged = self._left().merge(self._right())
        assert merged.needs_synthesis is True

    def test_merge_both_false_stays_false(self):
        left = self._left()
        right = self._right()
        right.needs_synthesis = False
        merged = left.merge(right)
        assert merged.needs_synthesis is False

    def test_merge_keeps_left_source_type(self):
        merged = self._left().merge(self._right())
        assert merged.source_type == "markdown"

    def test_merge_combines_metadata(self):
        merged = self._left().merge(self._right())
        assert merged.metadata["origin"] == "right"  # right overwrites left

    def test_merge_does_not_mutate_originals(self):
        left = self._left()
        right = self._right()
        left.merge(right)
        assert len(left.nodes) == 1
        assert len(right.nodes) == 1


# ---------------------------------------------------------------------------
# BaseExtractor ABC
# ---------------------------------------------------------------------------


class DummyExtractor(BaseExtractor):
    """Concrete test implementation of BaseExtractor."""

    @property
    def name(self) -> str:
        return "dummy"

    def can_handle(self, content_type: str, filename: str = "") -> bool:
        return content_type == "text/plain"

    def extract(self, content: str, filename: str = "", metadata: dict | None = None) -> ExtractionResult:
        return ExtractionResult(
            source_type="plain",
            content_type="text/plain",
            nodes=[{"label": "test", "kind": "entity", "summary": content[:50], "properties": {}}],
            raw_content=content,
        )


class TestBaseExtractor:
    """Verify BaseExtractor contract via DummyExtractor."""

    def test_name_property(self):
        ext = DummyExtractor()
        assert ext.name == "dummy"

    def test_can_handle_matching(self):
        ext = DummyExtractor()
        assert ext.can_handle("text/plain") is True

    def test_can_handle_non_matching(self):
        ext = DummyExtractor()
        assert ext.can_handle("application/json") is False

    def test_extract_returns_extraction_result(self):
        ext = DummyExtractor()
        result = ext.extract("hello world")
        assert isinstance(result, ExtractionResult)

    def test_extract_populates_nodes(self):
        ext = DummyExtractor()
        result = ext.extract("hello world")
        assert len(result.nodes) == 1
        assert result.nodes[0]["label"] == "test"

    def test_cannot_instantiate_abc_directly(self):
        with pytest.raises(TypeError):
            BaseExtractor()


# ---------------------------------------------------------------------------
# MarkdownExtractor
# ---------------------------------------------------------------------------

import sys
import os

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "knowledge"))
from services.knowledge.ingestion.extractors.markdown import MarkdownExtractor


class TestMarkdownExtractorCanHandle:
    """MarkdownExtractor.can_handle for various content types."""

    def test_handles_text_markdown(self):
        ext = MarkdownExtractor()
        assert ext.can_handle("text/markdown") is True

    def test_handles_text_plain(self):
        ext = MarkdownExtractor()
        assert ext.can_handle("text/plain") is True

    def test_rejects_application_json(self):
        ext = MarkdownExtractor()
        assert ext.can_handle("application/json") is False

    def test_rejects_text_html(self):
        ext = MarkdownExtractor()
        assert ext.can_handle("text/html") is False

    def test_name_is_markdown(self):
        ext = MarkdownExtractor()
        assert ext.name == "markdown"


class TestMarkdownExtractorHeadings:
    """Heading extraction produces concept nodes."""

    def test_single_heading(self):
        ext = MarkdownExtractor()
        result = ext.extract("# Overview\n")
        concepts = [n for n in result.nodes if n["kind"] == "concept"]
        assert len(concepts) == 1
        assert concepts[0]["label"] == "Overview"

    def test_multiple_headings(self):
        ext = MarkdownExtractor()
        result = ext.extract("# Top\n## Sub\n### Deep\n")
        concepts = [n for n in result.nodes if n["kind"] == "concept"]
        assert len(concepts) == 3

    def test_heading_level_in_properties(self):
        ext = MarkdownExtractor()
        result = ext.extract("## Details\n")
        concepts = [n for n in result.nodes if n["kind"] == "concept"]
        assert concepts[0]["properties"]["level"] == 2


class TestMarkdownExtractorHierarchy:
    """Parent-child heading relationships produce part_of edges."""

    def test_h2_under_h1_creates_part_of_edge(self):
        ext = MarkdownExtractor()
        result = ext.extract("# Parent\n## Child\n")
        part_of = [e for e in result.edges if e["relation"] == "part_of"]
        assert len(part_of) == 1
        assert part_of[0]["source_label"] == "Child"
        assert part_of[0]["target_label"] == "Parent"

    def test_h3_under_h2_creates_part_of_edge(self):
        ext = MarkdownExtractor()
        result = ext.extract("# Top\n## Mid\n### Bottom\n")
        part_of = [e for e in result.edges if e["relation"] == "part_of"]
        assert len(part_of) == 2
        bottom_edge = [e for e in part_of if e["source_label"] == "Bottom"]
        assert bottom_edge[0]["target_label"] == "Mid"

    def test_sibling_headings_no_edge_between_them(self):
        ext = MarkdownExtractor()
        result = ext.extract("# Top\n## A\n## B\n")
        part_of = [e for e in result.edges if e["relation"] == "part_of"]
        # Both A and B are children of Top
        assert all(e["target_label"] == "Top" for e in part_of)
        assert len(part_of) == 2


class TestMarkdownExtractorLinks:
    """Markdown links to .md files produce relates_to edges."""

    def test_md_link_creates_relates_to_edge(self):
        ext = MarkdownExtractor()
        result = ext.extract("# Intro\nSee [other doc](other.md) for details.\n")
        relates = [e for e in result.edges if e["relation"] == "relates_to"]
        assert len(relates) == 1
        assert relates[0]["target_label"] == "other.md"

    def test_non_md_link_no_relates_to_edge(self):
        ext = MarkdownExtractor()
        result = ext.extract("# Intro\nSee [site](https://example.com) for details.\n")
        relates = [e for e in result.edges if e["relation"] == "relates_to"]
        assert len(relates) == 0


class TestMarkdownExtractorURLs:
    """URL extraction produces url nodes."""

    def test_bare_url_creates_url_node(self):
        ext = MarkdownExtractor()
        result = ext.extract("Visit https://example.com for info.\n")
        urls = [n for n in result.nodes if n["kind"] == "url"]
        assert len(urls) == 1
        assert urls[0]["label"] == "https://example.com"

    def test_url_in_link_creates_url_node(self):
        ext = MarkdownExtractor()
        result = ext.extract("[Example](https://example.com/page)\n")
        urls = [n for n in result.nodes if n["kind"] == "url"]
        assert len(urls) == 1
        assert urls[0]["label"] == "https://example.com/page"

    def test_duplicate_urls_deduplicated(self):
        ext = MarkdownExtractor()
        result = ext.extract("https://example.com and https://example.com again\n")
        urls = [n for n in result.nodes if n["kind"] == "url"]
        assert len(urls) == 1


class TestMarkdownExtractorSynthesis:
    """needs_synthesis flag based on prose content."""

    def test_headings_only_no_synthesis(self):
        ext = MarkdownExtractor()
        result = ext.extract("# A\n## B\n### C\n")
        assert result.needs_synthesis is False

    def test_substantial_prose_triggers_synthesis(self):
        ext = MarkdownExtractor()
        prose = "# Title\n" + "This is a paragraph with substantial content. " * 20 + "\n"
        result = ext.extract(prose)
        assert result.needs_synthesis is True

    def test_short_prose_no_synthesis(self):
        ext = MarkdownExtractor()
        result = ext.extract("# Title\nShort note.\n")
        assert result.needs_synthesis is False


class TestMarkdownExtractorMetadata:
    """Metadata handling."""

    def test_source_file_in_metadata(self):
        ext = MarkdownExtractor()
        result = ext.extract("# Test\n", filename="README.md")
        assert result.metadata["source_file"] == "README.md"

    def test_passed_metadata_preserved(self):
        ext = MarkdownExtractor()
        result = ext.extract("# Test\n", filename="README.md", metadata={"custom": "val"})
        assert result.metadata["custom"] == "val"
        assert result.metadata["source_file"] == "README.md"

    def test_result_source_type_is_markdown(self):
        ext = MarkdownExtractor()
        result = ext.extract("# Test\n")
        assert result.source_type == "markdown"

    def test_raw_content_preserved(self):
        ext = MarkdownExtractor()
        content = "# Hello\nWorld\n"
        result = ext.extract(content)
        assert result.raw_content == content


# ---------------------------------------------------------------------------
# StructuredExtractor
# ---------------------------------------------------------------------------

from services.knowledge.ingestion.extractors.structured import StructuredExtractor


class TestStructuredExtractorCanHandle:
    """StructuredExtractor accepts any text/* content type."""

    def setup_method(self):
        self.ext = StructuredExtractor()

    def test_name(self):
        assert self.ext.name == "structured"

    def test_handles_text_plain(self):
        assert self.ext.can_handle("text/plain") is True

    def test_handles_text_html(self):
        assert self.ext.can_handle("text/html") is True

    def test_handles_text_markdown(self):
        assert self.ext.can_handle("text/markdown") is True

    def test_handles_text_csv(self):
        assert self.ext.can_handle("text/csv") is True

    def test_rejects_application_json(self):
        assert self.ext.can_handle("application/json") is False

    def test_rejects_image_png(self):
        assert self.ext.can_handle("image/png") is False


class TestStructuredExtractorIPv4:
    """IPv4 address extraction."""

    def setup_method(self):
        self.ext = StructuredExtractor()

    def test_single_ip(self):
        result = self.ext.extract("Found host at 192.168.1.1 in scan")
        labels = [n["label"] for n in result.nodes]
        assert "192.168.1.1" in labels

    def test_ip_node_kind_is_indicator(self):
        result = self.ext.extract("Host 10.0.0.1 responded")
        node = next(n for n in result.nodes if n["label"] == "10.0.0.1")
        assert node["kind"] == "indicator"

    def test_private_ip_detected(self):
        result = self.ext.extract("Internal: 192.168.0.5")
        node = next(n for n in result.nodes if n["label"] == "192.168.0.5")
        assert node["properties"]["visibility"] == "private"

    def test_public_ip_detected(self):
        result = self.ext.extract("External: 8.8.8.8")
        node = next(n for n in result.nodes if n["label"] == "8.8.8.8")
        assert node["properties"]["visibility"] == "public"

    def test_multiple_ips(self):
        result = self.ext.extract("Hosts: 10.0.0.1, 10.0.0.2, 8.8.4.4")
        labels = [n["label"] for n in result.nodes if n["kind"] == "indicator"]
        assert len(labels) == 3

    def test_invalid_octet_rejected(self):
        result = self.ext.extract("Not an IP: 999.999.999.999")
        ip_nodes = [n for n in result.nodes if n["kind"] == "indicator"]
        assert len(ip_nodes) == 0

    def test_octet_256_rejected(self):
        result = self.ext.extract("Edge case: 192.168.1.256")
        ip_nodes = [n for n in result.nodes if n["kind"] == "indicator"]
        assert len(ip_nodes) == 0


class TestStructuredExtractorCVE:
    """CVE ID extraction."""

    def setup_method(self):
        self.ext = StructuredExtractor()

    def test_single_cve(self):
        result = self.ext.extract("Patched CVE-2024-12345 in latest release")
        labels = [n["label"] for n in result.nodes]
        assert "CVE-2024-12345" in labels

    def test_cve_node_kind_is_vulnerability(self):
        result = self.ext.extract("CVE-2023-44487 is critical")
        node = next(n for n in result.nodes if n["label"] == "CVE-2023-44487")
        assert node["kind"] == "vulnerability"

    def test_five_digit_cve(self):
        result = self.ext.extract("See CVE-2021-44228 (Log4Shell)")
        labels = [n["label"] for n in result.nodes]
        assert "CVE-2021-44228" in labels

    def test_multiple_cves(self):
        result = self.ext.extract("CVE-2024-0001 and CVE-2024-0002 found")
        cve_nodes = [n for n in result.nodes if n["kind"] == "vulnerability"]
        assert len(cve_nodes) == 2


class TestStructuredExtractorURL:
    """URL extraction."""

    def setup_method(self):
        self.ext = StructuredExtractor()

    def test_https_url(self):
        result = self.ext.extract("Visit https://example.com/path")
        labels = [n["label"] for n in result.nodes]
        assert "https://example.com/path" in labels

    def test_http_url(self):
        result = self.ext.extract("Found http://internal.corp/api")
        labels = [n["label"] for n in result.nodes]
        assert "http://internal.corp/api" in labels

    def test_url_node_kind(self):
        result = self.ext.extract("See https://docs.example.com")
        node = next(n for n in result.nodes if n["label"] == "https://docs.example.com")
        assert node["kind"] == "url"

    def test_url_with_query_params(self):
        result = self.ext.extract("https://api.example.com/v1?key=val&foo=bar")
        labels = [n["label"] for n in result.nodes]
        assert "https://api.example.com/v1?key=val&foo=bar" in labels


class TestStructuredExtractorEmail:
    """Email address extraction."""

    def setup_method(self):
        self.ext = StructuredExtractor()

    def test_single_email(self):
        result = self.ext.extract("Contact admin@example.com for access")
        labels = [n["label"] for n in result.nodes]
        assert "admin@example.com" in labels

    def test_email_node_kind_is_contact(self):
        result = self.ext.extract("Owner: ops@corp.io")
        node = next(n for n in result.nodes if n["label"] == "ops@corp.io")
        assert node["kind"] == "contact"

    def test_complex_email(self):
        result = self.ext.extract("Send to first.last+tag@sub.domain.org")
        labels = [n["label"] for n in result.nodes]
        assert "first.last+tag@sub.domain.org" in labels


class TestStructuredExtractorHTTPStatus:
    """HTTP status code extraction as properties."""

    def setup_method(self):
        self.ext = StructuredExtractor()

    def test_http_status_in_metadata(self):
        result = self.ext.extract("Got HTTP 403 from server")
        assert "403" in result.metadata.get("http_status_codes", [])

    def test_status_code_pattern(self):
        result = self.ext.extract("status 500 returned")
        assert "500" in result.metadata.get("http_status_codes", [])

    def test_error_code_pattern(self):
        result = self.ext.extract("Server returned 404 Not Found")
        assert "404" in result.metadata.get("http_status_codes", [])


class TestStructuredExtractorDeduplication:
    """Duplicate entities are extracted only once."""

    def setup_method(self):
        self.ext = StructuredExtractor()

    def test_duplicate_ips(self):
        result = self.ext.extract("10.0.0.1 then 10.0.0.1 again 10.0.0.1")
        ip_nodes = [n for n in result.nodes if n["label"] == "10.0.0.1"]
        assert len(ip_nodes) == 1

    def test_duplicate_cves(self):
        result = self.ext.extract("CVE-2024-1234 is CVE-2024-1234")
        cve_nodes = [n for n in result.nodes if n["label"] == "CVE-2024-1234"]
        assert len(cve_nodes) == 1

    def test_duplicate_urls(self):
        result = self.ext.extract("https://a.com then https://a.com")
        url_nodes = [n for n in result.nodes if n["label"] == "https://a.com"]
        assert len(url_nodes) == 1

    def test_duplicate_emails(self):
        result = self.ext.extract("a@b.com and a@b.com")
        email_nodes = [n for n in result.nodes if n["label"] == "a@b.com"]
        assert len(email_nodes) == 1


class TestStructuredExtractorResult:
    """Result-level properties."""

    def setup_method(self):
        self.ext = StructuredExtractor()

    def test_needs_synthesis_always_true(self):
        result = self.ext.extract("Just some text with 10.0.0.1")
        assert result.needs_synthesis is True

    def test_needs_synthesis_true_even_empty(self):
        result = self.ext.extract("No entities here")
        assert result.needs_synthesis is True

    def test_source_type_is_structured(self):
        result = self.ext.extract("data")
        assert result.source_type == "structured"

    def test_raw_content_preserved(self):
        text = "Found CVE-2024-9999 at 10.0.0.1"
        result = self.ext.extract(text)
        assert result.raw_content == text

    def test_metadata_passed_through(self):
        result = self.ext.extract("text", metadata={"tool": "nmap"})
        assert result.metadata["tool"] == "nmap"


# ---------------------------------------------------------------------------
# ConfigExtractor
# ---------------------------------------------------------------------------

from services.knowledge.ingestion.extractors.config import ConfigExtractor


class TestConfigExtractorCanHandle:
    """ConfigExtractor.can_handle for various content types."""

    def test_handles_application_yaml(self):
        ext = ConfigExtractor()
        assert ext.can_handle("application/yaml") is True

    def test_handles_application_json(self):
        ext = ConfigExtractor()
        assert ext.can_handle("application/json") is True

    def test_handles_application_toml(self):
        ext = ConfigExtractor()
        assert ext.can_handle("application/toml") is True

    def test_rejects_text_plain(self):
        ext = ConfigExtractor()
        assert ext.can_handle("text/plain") is False

    def test_rejects_text_markdown(self):
        ext = ConfigExtractor()
        assert ext.can_handle("text/markdown") is False

    def test_name_is_config(self):
        ext = ConfigExtractor()
        assert ext.name == "config"

    def test_handles_yaml_by_filename(self):
        ext = ConfigExtractor()
        assert ext.can_handle("application/octet-stream", "config.yaml") is True

    def test_handles_yml_by_filename(self):
        ext = ConfigExtractor()
        assert ext.can_handle("application/octet-stream", "config.yml") is True

    def test_handles_json_by_filename(self):
        ext = ConfigExtractor()
        assert ext.can_handle("application/octet-stream", "settings.json") is True

    def test_handles_toml_by_filename(self):
        ext = ConfigExtractor()
        assert ext.can_handle("application/octet-stream", "pyproject.toml") is True


class TestConfigExtractorYAML:
    """YAML key extraction."""

    def test_top_level_keys_become_nodes(self):
        ext = ConfigExtractor()
        result = ext.extract("name: myapp\nversion: 1.0\n", metadata={"content_type": "application/yaml"})
        labels = [n["label"] for n in result.nodes if n["kind"] == "config_item"]
        assert "name" in labels
        assert "version" in labels

    def test_nested_keys_create_part_of_edges(self):
        ext = ConfigExtractor()
        content = "database:\n  host: localhost\n  port: 5432\n"
        result = ext.extract(content, metadata={"content_type": "application/yaml"})
        part_of = [e for e in result.edges if e["relation"] == "part_of"]
        assert len(part_of) >= 2
        host_edge = [e for e in part_of if e["source_label"] == "database.host"]
        assert host_edge[0]["target_label"] == "database"

    def test_source_type_is_config(self):
        ext = ConfigExtractor()
        result = ext.extract("key: val\n", metadata={"content_type": "application/yaml"})
        assert result.source_type == "config"


class TestConfigExtractorJSON:
    """JSON key extraction."""

    def test_top_level_keys_become_nodes(self):
        ext = ConfigExtractor()
        result = ext.extract('{"name": "myapp", "version": "1.0"}', metadata={"content_type": "application/json"})
        labels = [n["label"] for n in result.nodes if n["kind"] == "config_item"]
        assert "name" in labels
        assert "version" in labels

    def test_nested_keys_create_part_of_edges(self):
        ext = ConfigExtractor()
        content = '{"database": {"host": "localhost", "port": 5432}}'
        result = ext.extract(content, metadata={"content_type": "application/json"})
        part_of = [e for e in result.edges if e["relation"] == "part_of"]
        host_edge = [e for e in part_of if e["source_label"] == "database.host"]
        assert len(host_edge) == 1
        assert host_edge[0]["target_label"] == "database"


class TestConfigExtractorNeedsSynthesis:
    """Config extractor always sets needs_synthesis=False."""

    def test_yaml_needs_synthesis_false(self):
        ext = ConfigExtractor()
        result = ext.extract("key: val\n", metadata={"content_type": "application/yaml"})
        assert result.needs_synthesis is False

    def test_json_needs_synthesis_false(self):
        ext = ConfigExtractor()
        result = ext.extract('{"key": "val"}', metadata={"content_type": "application/json"})
        assert result.needs_synthesis is False


class TestConfigExtractorURLs:
    """URL values produce url nodes."""

    def test_url_value_creates_url_node(self):
        ext = ConfigExtractor()
        content = "api_base: https://api.example.com/v1\n"
        result = ext.extract(content, metadata={"content_type": "application/yaml"})
        urls = [n for n in result.nodes if n["kind"] == "url"]
        assert len(urls) == 1
        assert urls[0]["label"] == "https://api.example.com/v1"

    def test_multiple_urls_extracted(self):
        ext = ConfigExtractor()
        content = '{"primary": "https://a.com", "secondary": "https://b.com"}'
        result = ext.extract(content, metadata={"content_type": "application/json"})
        urls = [n for n in result.nodes if n["kind"] == "url"]
        assert len(urls) == 2

    def test_non_url_string_no_url_node(self):
        ext = ConfigExtractor()
        content = "name: myapp\n"
        result = ext.extract(content, metadata={"content_type": "application/yaml"})
        urls = [n for n in result.nodes if n["kind"] == "url"]
        assert len(urls) == 0


class TestConfigExtractorNestedStructure:
    """Deeply nested structure edges."""

    def test_three_levels_deep(self):
        ext = ConfigExtractor()
        content = "a:\n  b:\n    c: val\n"
        result = ext.extract(content, metadata={"content_type": "application/yaml"})
        part_of = [e for e in result.edges if e["relation"] == "part_of"]
        c_edge = [e for e in part_of if e["source_label"] == "a.b.c"]
        assert c_edge[0]["target_label"] == "a.b"
        b_edge = [e for e in part_of if e["source_label"] == "a.b"]
        assert b_edge[0]["target_label"] == "a"

    def test_list_values_do_not_create_child_nodes(self):
        ext = ConfigExtractor()
        content = "tags:\n  - alpha\n  - beta\n"
        result = ext.extract(content, metadata={"content_type": "application/yaml"})
        labels = [n["label"] for n in result.nodes if n["kind"] == "config_item"]
        assert "tags" in labels
        assert len(labels) == 1


class TestConfigExtractorParseErrors:
    """Graceful handling of parse errors."""

    def test_invalid_yaml_returns_empty_result(self):
        ext = ConfigExtractor()
        result = ext.extract("{{invalid yaml", metadata={"content_type": "application/yaml"})
        assert result.nodes == []
        assert result.edges == []
        assert "error" in result.metadata

    def test_invalid_json_returns_empty_result(self):
        ext = ConfigExtractor()
        result = ext.extract("{bad json", metadata={"content_type": "application/json"})
        assert result.nodes == []
        assert "error" in result.metadata


class TestConfigExtractorTOML:
    """TOML parsing."""

    def test_toml_top_level_keys(self):
        ext = ConfigExtractor()
        content = 'name = "myapp"\nversion = "1.0"\n'
        result = ext.extract(content, metadata={"content_type": "application/toml"})
        labels = [n["label"] for n in result.nodes if n["kind"] == "config_item"]
        assert "name" in labels
        assert "version" in labels

    def test_toml_nested_table(self):
        ext = ConfigExtractor()
        content = '[database]\nhost = "localhost"\nport = 5432\n'
        result = ext.extract(content, metadata={"content_type": "application/toml"})
        part_of = [e for e in result.edges if e["relation"] == "part_of"]
        host_edge = [e for e in part_of if e["source_label"] == "database.host"]
        assert len(host_edge) == 1
        assert host_edge[0]["target_label"] == "database"
