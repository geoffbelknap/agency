import os
import tempfile
import pytest
from images.egress.key_resolver import FileKeyResolver


class TestFileKeyResolver:
    def test_resolve_existing_key(self, tmp_path):
        keys_file = tmp_path / ".service-keys.env"
        keys_file.write_text("ANTHROPIC_API_KEY=sk-ant-123\nnextdns-api=abc456\n")
        resolver = FileKeyResolver(str(keys_file))
        assert resolver.resolve("ANTHROPIC_API_KEY") == "sk-ant-123"
        assert resolver.resolve("nextdns-api") == "abc456"

    def test_resolve_missing_key_returns_none(self, tmp_path):
        keys_file = tmp_path / ".service-keys.env"
        keys_file.write_text("ANTHROPIC_API_KEY=sk-ant-123\n")
        resolver = FileKeyResolver(str(keys_file))
        assert resolver.resolve("NONEXISTENT") is None

    def test_resolve_skips_comments_and_blanks(self, tmp_path):
        keys_file = tmp_path / ".service-keys.env"
        keys_file.write_text("# comment\n\nKEY=val\n")
        resolver = FileKeyResolver(str(keys_file))
        assert resolver.resolve("KEY") == "val"

    def test_resolve_handles_equals_in_value(self, tmp_path):
        keys_file = tmp_path / ".service-keys.env"
        keys_file.write_text("TOKEN=abc=def=ghi\n")
        resolver = FileKeyResolver(str(keys_file))
        assert resolver.resolve("TOKEN") == "abc=def=ghi"

    def test_reload(self, tmp_path):
        keys_file = tmp_path / ".service-keys.env"
        keys_file.write_text("KEY=old\n")
        resolver = FileKeyResolver(str(keys_file))
        assert resolver.resolve("KEY") == "old"
        keys_file.write_text("KEY=new\n")
        resolver.reload()
        assert resolver.resolve("KEY") == "new"

    def test_missing_file_returns_none(self, tmp_path):
        resolver = FileKeyResolver(str(tmp_path / "nonexistent"))
        assert resolver.resolve("KEY") is None
