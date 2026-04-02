"""Tests for build-time image profiling."""

from unittest.mock import MagicMock, patch

import pytest

from images.profile_image import label_image, profile_and_label, profile_image_rss


@pytest.fixture
def mock_client():
    return MagicMock()


@pytest.fixture
def mock_container():
    container = MagicMock()
    container.stats.return_value = {
        "memory_stats": {"usage": 52_428_800}  # 50 MB
    }
    return container


class TestProfileImageRss:
    def test_returns_mb(self, mock_client, mock_container):
        mock_client.containers.run.return_value = mock_container
        with patch("images.profile_image.time"):
            result = profile_image_rss(mock_client, "test:latest")
        assert result == 50

    def test_returns_1_for_small_usage(self, mock_client):
        container = MagicMock()
        # 1.5 MB -- integer division gives 1, and max(1, 1) == 1
        container.stats.return_value = {"memory_stats": {"usage": 1_572_864}}
        mock_client.containers.run.return_value = container
        with patch("images.profile_image.time"):
            result = profile_image_rss(mock_client, "test:latest")
        assert result == 1  # min clamp to 1

    def test_handles_run_failure(self, mock_client):
        mock_client.containers.run.side_effect = Exception("image not found")
        result = profile_image_rss(mock_client, "bad:latest")
        assert result == 0

    def test_handles_stats_failure(self, mock_client):
        container = MagicMock()
        container.stats.side_effect = Exception("stats failed")
        mock_client.containers.run.return_value = container
        with patch("images.profile_image.time"):
            result = profile_image_rss(mock_client, "test:latest")
        assert result == 0
        container.stop.assert_called_once_with(timeout=5)
        container.remove.assert_called_once_with(force=True)

    def test_handles_zero_usage(self, mock_client):
        container = MagicMock()
        container.stats.return_value = {"memory_stats": {"usage": 0}}
        mock_client.containers.run.return_value = container
        with patch("images.profile_image.time"):
            result = profile_image_rss(mock_client, "test:latest")
        assert result == 0

    def test_handles_missing_memory_stats(self, mock_client):
        container = MagicMock()
        container.stats.return_value = {}
        mock_client.containers.run.return_value = container
        with patch("images.profile_image.time"):
            result = profile_image_rss(mock_client, "test:latest")
        assert result == 0

    def test_cleans_up_container(self, mock_client, mock_container):
        mock_client.containers.run.return_value = mock_container
        with patch("images.profile_image.time"):
            profile_image_rss(mock_client, "test:latest")
        mock_container.stop.assert_called_once_with(timeout=5)
        mock_container.remove.assert_called_once_with(force=True)

    def test_cleans_up_on_stats_exception(self, mock_client):
        container = MagicMock()
        container.stats.side_effect = RuntimeError("boom")
        mock_client.containers.run.return_value = container
        with patch("images.profile_image.time"):
            profile_image_rss(mock_client, "test:latest")
        container.stop.assert_called_once_with(timeout=5)
        container.remove.assert_called_once_with(force=True)

    def test_passes_image_tag_and_options(self, mock_client, mock_container):
        mock_client.containers.run.return_value = mock_container
        with patch("images.profile_image.time"):
            profile_image_rss(mock_client, "myimage:v1", timeout=15)
        mock_client.containers.run.assert_called_once_with(
            image="myimage:v1",
            detach=True,
            mem_limit="1g",
            remove=False,
        )


class TestLabelImage:
    def test_builds_from_tag(self, mock_client):
        label_image(mock_client, "test:latest", {"agency.baseline_rss_mb": "50"})
        mock_client.images.build.assert_called_once()
        call_kwargs = mock_client.images.build.call_args[1]
        assert call_kwargs["tag"] == "test:latest"
        assert call_kwargs["rm"] is True
        assert call_kwargs["quiet"] is True

    def test_dockerfile_content(self, mock_client):
        with patch("images.profile_image.tempfile.TemporaryDirectory") as tmp_cls:
            tmp_cls.return_value.__enter__ = MagicMock(return_value="/tmp/fakedir")
            tmp_cls.return_value.__exit__ = MagicMock(return_value=False)
            mock_open = MagicMock()
            with patch("builtins.open", mock_open):
                label_image(mock_client, "test:latest", {"agency.baseline_rss_mb": "42"})
            written = mock_open().__enter__().write.call_args[0][0]
            assert "FROM test:latest" in written
            assert 'agency.baseline_rss_mb="42"' in written


class TestProfileAndLabel:
    def test_integrates_profile_and_label(self, mock_client):
        with patch("images.profile_image.profile_image_rss", return_value=50) as mock_rss, \
             patch("images.profile_image.label_image") as mock_label:
            result = profile_and_label(mock_client, "test:latest")
        assert result == 50
        mock_rss.assert_called_once_with(mock_client, "test:latest", timeout=10)
        mock_label.assert_called_once_with(
            mock_client, "test:latest", {"agency.baseline_rss_mb": "50"}
        )

    def test_skips_label_on_zero(self, mock_client):
        with patch("images.profile_image.profile_image_rss", return_value=0) as mock_rss, \
             patch("images.profile_image.label_image") as mock_label:
            result = profile_and_label(mock_client, "test:latest")
        assert result == 0
        mock_rss.assert_called_once()
        mock_label.assert_not_called()

    def test_passes_timeout(self, mock_client):
        with patch("images.profile_image.profile_image_rss", return_value=25) as mock_rss, \
             patch("images.profile_image.label_image"):
            profile_and_label(mock_client, "test:latest", timeout=20)
        mock_rss.assert_called_once_with(mock_client, "test:latest", timeout=20)
