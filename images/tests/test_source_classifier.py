"""Tests for SourceClassifier — content type detection for knowledge ingestion."""

from services.knowledge.ingestion.classifier import SourceClassifier


class TestClassifyByExtension:
    def test_markdown(self):
        assert SourceClassifier.classify(filename="README.md") == "text/markdown"

    def test_yaml(self):
        assert SourceClassifier.classify(filename="config.yaml") == "text/yaml"

    def test_yml(self):
        assert SourceClassifier.classify(filename="config.yml") == "text/yaml"

    def test_json(self):
        assert SourceClassifier.classify(filename="data.json") == "application/json"

    def test_python(self):
        assert SourceClassifier.classify(filename="main.py") == "text/x-python"

    def test_go(self):
        assert SourceClassifier.classify(filename="main.go") == "text/x-go"

    def test_html(self):
        assert SourceClassifier.classify(filename="index.html") == "text/html"

    def test_toml(self):
        assert SourceClassifier.classify(filename="config.toml") == "text/toml"

    def test_txt(self):
        assert SourceClassifier.classify(filename="notes.txt") == "text/plain"

    def test_javascript(self):
        assert SourceClassifier.classify(filename="app.js") == "text/javascript"

    def test_typescript(self):
        assert SourceClassifier.classify(filename="app.ts") == "text/typescript"

    def test_java(self):
        assert SourceClassifier.classify(filename="Main.java") == "text/x-java"

    def test_rust(self):
        assert SourceClassifier.classify(filename="lib.rs") == "text/x-rust"

    def test_ruby(self):
        assert SourceClassifier.classify(filename="app.rb") == "text/x-ruby"

    def test_c(self):
        assert SourceClassifier.classify(filename="main.c") == "text/x-c"

    def test_cpp(self):
        assert SourceClassifier.classify(filename="main.cpp") == "text/x-c++"

    def test_header(self):
        assert SourceClassifier.classify(filename="lib.h") == "text/x-c"

    def test_pdf(self):
        assert SourceClassifier.classify(filename="doc.pdf") == "application/pdf"

    def test_png(self):
        assert SourceClassifier.classify(filename="logo.png") == "image/png"

    def test_jpg(self):
        assert SourceClassifier.classify(filename="photo.jpg") == "image/jpeg"

    def test_shell(self):
        assert SourceClassifier.classify(filename="run.sh") == "text/x-shellscript"


class TestClassifyByExplicitType:
    def test_explicit_type_takes_priority(self):
        result = SourceClassifier.classify(
            filename="data.txt", content_type="application/json"
        )
        assert result == "application/json"

    def test_explicit_type_over_content(self):
        result = SourceClassifier.classify(
            content_type="text/yaml", content='{"key": "value"}'
        )
        assert result == "text/yaml"


class TestClassifyByURL:
    def test_http_url(self):
        assert SourceClassifier.classify(filename="http://example.com") == "text/html"

    def test_https_url(self):
        result = SourceClassifier.classify(filename="https://example.com/page")
        assert result == "text/html"

    def test_url_priority_below_explicit_type(self):
        result = SourceClassifier.classify(
            filename="https://example.com/api/data",
            content_type="application/json",
        )
        assert result == "application/json"


class TestClassifyByContentSniffing:
    def test_yaml_frontmatter(self):
        result = SourceClassifier.classify(content="---\ntitle: hello\n---\n")
        assert result == "text/yaml"

    def test_yaml_key_value(self):
        result = SourceClassifier.classify(content="key: value\nother: stuff\n")
        assert result == "text/yaml"

    def test_json_object(self):
        result = SourceClassifier.classify(content='{"key": "value"}')
        assert result == "application/json"

    def test_json_array(self):
        result = SourceClassifier.classify(content='[1, 2, 3]')
        assert result == "application/json"

    def test_html_doctype(self):
        result = SourceClassifier.classify(content="<!DOCTYPE html>\n<html>")
        assert result == "text/html"

    def test_html_tag(self):
        result = SourceClassifier.classify(content="<html>\n<body>hello</body></html>")
        assert result == "text/html"


class TestUnknownDefault:
    def test_no_inputs(self):
        assert SourceClassifier.classify() == "text/plain"

    def test_unknown_extension(self):
        assert SourceClassifier.classify(filename="data.xyz") == "text/plain"

    def test_unrecognized_content(self):
        result = SourceClassifier.classify(content="just some random text")
        assert result == "text/plain"


class TestIsCode:
    def test_python_is_code(self):
        assert SourceClassifier.is_code("text/x-python") is True

    def test_go_is_code(self):
        assert SourceClassifier.is_code("text/x-go") is True

    def test_javascript_is_code(self):
        assert SourceClassifier.is_code("text/javascript") is True

    def test_typescript_is_code(self):
        assert SourceClassifier.is_code("text/typescript") is True

    def test_java_is_code(self):
        assert SourceClassifier.is_code("text/x-java") is True

    def test_rust_is_code(self):
        assert SourceClassifier.is_code("text/x-rust") is True

    def test_ruby_is_code(self):
        assert SourceClassifier.is_code("text/x-ruby") is True

    def test_c_is_code(self):
        assert SourceClassifier.is_code("text/x-c") is True

    def test_cpp_is_code(self):
        assert SourceClassifier.is_code("text/x-c++") is True

    def test_shell_is_code(self):
        assert SourceClassifier.is_code("text/x-shellscript") is True

    def test_markdown_is_not_code(self):
        assert SourceClassifier.is_code("text/markdown") is False

    def test_json_is_not_code(self):
        assert SourceClassifier.is_code("application/json") is False


class TestIsConfig:
    def test_yaml_is_config(self):
        assert SourceClassifier.is_config("text/yaml") is True

    def test_json_is_config(self):
        assert SourceClassifier.is_config("application/json") is True

    def test_toml_is_config(self):
        assert SourceClassifier.is_config("text/toml") is True

    def test_python_is_not_config(self):
        assert SourceClassifier.is_config("text/x-python") is False

    def test_html_is_not_config(self):
        assert SourceClassifier.is_config("text/html") is False


class TestIsImage:
    def test_png_is_image(self):
        assert SourceClassifier.is_image("image/png") is True

    def test_jpeg_is_image(self):
        assert SourceClassifier.is_image("image/jpeg") is True

    def test_pdf_is_not_image(self):
        assert SourceClassifier.is_image("application/pdf") is False

    def test_text_is_not_image(self):
        assert SourceClassifier.is_image("text/plain") is False
