"""Tests for CodeExtractor — code file extraction for the knowledge graph."""

import os
import sys

import pytest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "knowledge"))
from ingestion.extractors.code import CodeExtractor


# ---------------------------------------------------------------------------
# can_handle
# ---------------------------------------------------------------------------


class TestCodeExtractorCanHandle:
    """CodeExtractor.can_handle for various content types."""

    def setup_method(self):
        self.ext = CodeExtractor()

    def test_name_is_code(self):
        assert self.ext.name == "code"

    def test_handles_python(self):
        assert self.ext.can_handle("text/x-python") is True

    def test_handles_go(self):
        assert self.ext.can_handle("text/x-go") is True

    def test_handles_javascript(self):
        assert self.ext.can_handle("text/javascript") is True

    def test_handles_typescript(self):
        assert self.ext.can_handle("text/typescript") is True

    def test_handles_java(self):
        assert self.ext.can_handle("text/x-java") is True

    def test_handles_rust(self):
        assert self.ext.can_handle("text/x-rust") is True

    def test_rejects_markdown(self):
        assert self.ext.can_handle("text/markdown") is False

    def test_rejects_plain_text(self):
        assert self.ext.can_handle("text/plain") is False

    def test_rejects_html(self):
        assert self.ext.can_handle("text/html") is False

    def test_rejects_json(self):
        assert self.ext.can_handle("application/json") is False


# ---------------------------------------------------------------------------
# Python extraction
# ---------------------------------------------------------------------------


class TestCodeExtractorPython:
    """Python function and class extraction."""

    def setup_method(self):
        self.ext = CodeExtractor()

    def test_function_extraction(self):
        code = "def hello_world():\n    pass\n"
        result = self.ext.extract(code, filename="hello.py", metadata={"content_type": "text/x-python"})
        funcs = [n for n in result.nodes if n["kind"] == "function"]
        assert len(funcs) == 1
        assert funcs[0]["label"] == "hello_world"

    def test_multiple_functions(self):
        code = "def foo():\n    pass\n\ndef bar():\n    pass\n"
        result = self.ext.extract(code, filename="mod.py", metadata={"content_type": "text/x-python"})
        funcs = [n for n in result.nodes if n["kind"] == "function"]
        assert len(funcs) == 2
        labels = {n["label"] for n in funcs}
        assert labels == {"foo", "bar"}

    def test_class_extraction(self):
        code = "class MyService:\n    pass\n"
        result = self.ext.extract(code, filename="svc.py", metadata={"content_type": "text/x-python"})
        classes = [n for n in result.nodes if n["kind"] == "system"]
        assert len(classes) == 1
        assert classes[0]["label"] == "MyService"

    def test_class_and_function(self):
        code = "class Foo:\n    pass\n\ndef bar():\n    pass\n"
        result = self.ext.extract(code, filename="mod.py", metadata={"content_type": "text/x-python"})
        assert len(result.nodes) == 2

    def test_import_in_metadata(self):
        code = "import os\nfrom pathlib import Path\n\ndef main():\n    pass\n"
        result = self.ext.extract(code, filename="app.py", metadata={"content_type": "text/x-python"})
        assert "imports" in result.metadata
        assert "os" in result.metadata["imports"]
        assert "pathlib" in result.metadata["imports"]

    def test_function_has_language_property(self):
        code = "def greet():\n    pass\n"
        result = self.ext.extract(code, filename="g.py", metadata={"content_type": "text/x-python"})
        func = result.nodes[0]
        assert func["properties"]["language"] == "python"

    def test_function_has_source_file(self):
        code = "def greet():\n    pass\n"
        result = self.ext.extract(code, filename="g.py", metadata={"content_type": "text/x-python"})
        func = result.nodes[0]
        assert func["properties"]["source_file"] == "g.py"

    def test_function_has_line_number(self):
        code = "# comment\n\ndef greet():\n    pass\n"
        result = self.ext.extract(code, filename="g.py", metadata={"content_type": "text/x-python"})
        func = result.nodes[0]
        assert func["properties"]["line_number"] == 3


# ---------------------------------------------------------------------------
# Go extraction
# ---------------------------------------------------------------------------


class TestCodeExtractorGo:
    """Go function and struct extraction."""

    def setup_method(self):
        self.ext = CodeExtractor()

    def test_function_extraction(self):
        code = "func main() {\n}\n"
        result = self.ext.extract(code, filename="main.go", metadata={"content_type": "text/x-go"})
        funcs = [n for n in result.nodes if n["kind"] == "function"]
        assert len(funcs) == 1
        assert funcs[0]["label"] == "main"

    def test_method_extraction(self):
        code = "func (s *Server) Start() {\n}\n"
        result = self.ext.extract(code, filename="server.go", metadata={"content_type": "text/x-go"})
        funcs = [n for n in result.nodes if n["kind"] == "function"]
        assert len(funcs) == 1
        assert funcs[0]["label"] == "Start"

    def test_struct_extraction(self):
        code = "type Config struct {\n\tName string\n}\n"
        result = self.ext.extract(code, filename="config.go", metadata={"content_type": "text/x-go"})
        structs = [n for n in result.nodes if n["kind"] == "system"]
        assert len(structs) == 1
        assert structs[0]["label"] == "Config"

    def test_interface_extraction(self):
        code = "type Handler interface {\n\tServe()\n}\n"
        result = self.ext.extract(code, filename="handler.go", metadata={"content_type": "text/x-go"})
        ifaces = [n for n in result.nodes if n["kind"] == "system"]
        assert len(ifaces) == 1
        assert ifaces[0]["label"] == "Handler"

    def test_go_language_property(self):
        code = "func init() {\n}\n"
        result = self.ext.extract(code, filename="main.go", metadata={"content_type": "text/x-go"})
        assert result.nodes[0]["properties"]["language"] == "go"


# ---------------------------------------------------------------------------
# JavaScript / TypeScript extraction
# ---------------------------------------------------------------------------


class TestCodeExtractorJavaScript:
    """JavaScript and TypeScript extraction."""

    def setup_method(self):
        self.ext = CodeExtractor()

    def test_function_declaration(self):
        code = "function greet() {\n}\n"
        result = self.ext.extract(code, filename="app.js", metadata={"content_type": "text/javascript"})
        funcs = [n for n in result.nodes if n["kind"] == "function"]
        assert len(funcs) == 1
        assert funcs[0]["label"] == "greet"

    def test_exported_function(self):
        code = "export function handleRequest() {\n}\n"
        result = self.ext.extract(code, filename="handler.js", metadata={"content_type": "text/javascript"})
        funcs = [n for n in result.nodes if n["kind"] == "function"]
        assert len(funcs) == 1
        assert funcs[0]["label"] == "handleRequest"

    def test_arrow_function(self):
        code = "const process = () => {\n}\n"
        result = self.ext.extract(code, filename="util.js", metadata={"content_type": "text/javascript"})
        funcs = [n for n in result.nodes if n["kind"] == "function"]
        assert len(funcs) == 1
        assert funcs[0]["label"] == "process"

    def test_const_function_expression(self):
        code = "const handler = function() {\n}\n"
        result = self.ext.extract(code, filename="util.js", metadata={"content_type": "text/javascript"})
        funcs = [n for n in result.nodes if n["kind"] == "function"]
        assert len(funcs) == 1
        assert funcs[0]["label"] == "handler"

    def test_class_extraction(self):
        code = "class MyComponent {\n}\n"
        result = self.ext.extract(code, filename="comp.js", metadata={"content_type": "text/javascript"})
        classes = [n for n in result.nodes if n["kind"] == "system"]
        assert len(classes) == 1
        assert classes[0]["label"] == "MyComponent"

    def test_exported_class(self):
        code = "export class Router {\n}\n"
        result = self.ext.extract(code, filename="router.ts", metadata={"content_type": "text/typescript"})
        classes = [n for n in result.nodes if n["kind"] == "system"]
        assert len(classes) == 1
        assert classes[0]["label"] == "Router"

    def test_typescript_language_property(self):
        code = "function run() {\n}\n"
        result = self.ext.extract(code, filename="run.ts", metadata={"content_type": "text/typescript"})
        assert result.nodes[0]["properties"]["language"] == "typescript"

    def test_javascript_language_property(self):
        code = "function run() {\n}\n"
        result = self.ext.extract(code, filename="run.js", metadata={"content_type": "text/javascript"})
        assert result.nodes[0]["properties"]["language"] == "javascript"


# ---------------------------------------------------------------------------
# Synthesis decision
# ---------------------------------------------------------------------------


class TestCodeExtractorSynthesis:
    """needs_synthesis based on comment/docstring volume."""

    def setup_method(self):
        self.ext = CodeExtractor()

    def test_bare_code_no_synthesis(self):
        code = "def foo():\n    pass\n\ndef bar():\n    pass\n"
        result = self.ext.extract(code, filename="bare.py", metadata={"content_type": "text/x-python"})
        assert result.needs_synthesis is False

    def test_heavy_docstrings_triggers_synthesis(self):
        docstring = '    """' + "This function does a lot of important work. " * 10 + '"""\n'
        code = f"def foo():\n{docstring}    pass\n"
        result = self.ext.extract(code, filename="documented.py", metadata={"content_type": "text/x-python"})
        assert result.needs_synthesis is True

    def test_heavy_comments_triggers_synthesis(self):
        comments = "\n".join(f"# This is comment line {i} with some explanation text" for i in range(20))
        code = f"{comments}\ndef foo():\n    pass\n"
        result = self.ext.extract(code, filename="commented.py", metadata={"content_type": "text/x-python"})
        assert result.needs_synthesis is True


# ---------------------------------------------------------------------------
# Default provenance
# ---------------------------------------------------------------------------


class TestCodeExtractorProvenance:
    """Default provenance is EXTRACTED."""

    def test_default_provenance(self):
        ext = CodeExtractor()
        code = "def foo():\n    pass\n"
        result = ext.extract(code, filename="f.py", metadata={"content_type": "text/x-python"})
        assert result.default_provenance == "EXTRACTED"
