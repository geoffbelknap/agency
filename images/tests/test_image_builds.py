"""Tests for parallel image building."""

from unittest.mock import MagicMock, patch

import docker.errors

from images import (
    BUILDABLE,
    BuildResult,
    OPTIONAL_IMAGES,
    _image_exists,
    any_images_missing,
    build_image_streaming,
    build_images_parallel,
    ensure_images,
    image_tag,
)


def _make_client(existing_tags=None):
    """Create a mock image client with optional pre-existing images."""
    existing = set(existing_tags or [])
    client = MagicMock()

    def get_image(tag):
        if tag in existing:
            return MagicMock()
        raise docker.errors.ImageNotFound(f"No such image: {tag}")

    client.images.get.side_effect = get_image
    client.images.build.return_value = (MagicMock(), [])
    return client


def test_skips_existing_images():
    """Existing images are not rebuilt when force=False."""
    all_tags = [image_tag(c) for c in BUILDABLE]
    client = _make_client(existing_tags=all_tags)

    results, warnings = build_images_parallel(client)

    client.images.build.assert_not_called()
    assert warnings == []
    for comp, result in results.items():
        assert not result.built
        assert result.tag == image_tag(comp)


def test_builds_missing_images():
    """Missing images trigger a build."""
    client = _make_client(existing_tags=[])

    with patch("images.build_image") as mock_build:
        results, warnings = build_images_parallel(client)

    assert mock_build.call_count == len(BUILDABLE)
    assert warnings == []
    for result in results.values():
        assert result.built


def test_force_rebuilds_existing():
    """force=True rebuilds all images even if they exist."""
    all_tags = [image_tag(c) for c in BUILDABLE]
    client = _make_client(existing_tags=all_tags)

    with patch("images.build_image") as mock_build:
        results, warnings = build_images_parallel(client, force=True)

    assert mock_build.call_count == len(BUILDABLE)
    for result in results.values():
        assert result.built


def test_callbacks_called():
    """on_start and on_complete callbacks are invoked for each built image."""
    client = _make_client(existing_tags=[])
    started = []
    completed = []

    with patch("images.build_image"):
        build_images_parallel(
            client,
            on_start=lambda c: started.append(c),
            on_complete=lambda r: completed.append(r),
        )

    assert len(started) == len(BUILDABLE)
    assert len(completed) == len(BUILDABLE)
    for r in completed:
        assert isinstance(r, BuildResult)


def test_error_aggregation():
    """Build errors for required images are collected and raised together."""
    client = _make_client(existing_tags=[])
    required_count = len([c for c in BUILDABLE if c not in OPTIONAL_IMAGES])

    with patch("images.build_image", side_effect=RuntimeError("build failed")):
        try:
            build_images_parallel(client)
            assert False, "Should have raised"
        except Exception as e:
            assert "build failed" in str(e)
            assert str(required_count) in str(e)


def test_optional_image_failure_warns():
    """Optional image build failures produce warnings instead of errors."""
    client = _make_client(existing_tags=[])

    def selective_build(client, component, quiet=False, profile=False):
        if component in OPTIONAL_IMAGES:
            raise RuntimeError(f"{component} unavailable")

    with patch("images.build_image", side_effect=selective_build):
        results, warnings = build_images_parallel(client)

    # Required images built successfully
    required = [c for c in BUILDABLE if c not in OPTIONAL_IMAGES]
    for comp in required:
        assert comp in results
        assert results[comp].built

    # Optional images produced warnings, not errors
    assert len(warnings) == len(OPTIONAL_IMAGES)
    for warn in warnings:
        assert "unavailable" in warn


def test_only_optional_failures_no_raise():
    """When only optional images fail, no exception is raised."""
    # Pre-build all required images so they are skipped
    required_tags = [image_tag(c) for c in BUILDABLE if c not in OPTIONAL_IMAGES]
    client = _make_client(existing_tags=required_tags)

    def fail_optional(client, component, quiet=False):
        raise RuntimeError("not available")

    with patch("images.build_image", side_effect=fail_optional):
        results, warnings = build_images_parallel(client)

    assert len(warnings) == len(OPTIONAL_IMAGES)
    # No exception raised — test passes if we reach here


def test_defaults_to_all_buildable():
    """None components builds all BUILDABLE."""
    all_tags = [image_tag(c) for c in BUILDABLE]
    client = _make_client(existing_tags=all_tags)

    results, warnings = build_images_parallel(client, components=None)

    assert set(results.keys()) == set(BUILDABLE)


def test_ensure_images_returns_dict():
    """ensure_images returns {component: tag} dict."""
    all_tags = [image_tag(c) for c in BUILDABLE]
    client = _make_client(existing_tags=all_tags)

    results = ensure_images(client)

    assert isinstance(results, dict)
    for comp, tag in results.items():
        assert tag == image_tag(comp)


def test_ensure_images_uses_parallel():
    """ensure_images delegates to build_images_parallel."""
    client = MagicMock()

    with patch("images.build_images_parallel") as mock_parallel:
        mock_parallel.return_value = (
            {"egress": BuildResult(component="egress", tag="agency-egress:latest", built=False)},
            [],
        )
        results = ensure_images(client, components=["egress"])

    mock_parallel.assert_called_once_with(client, components=["egress"], force=False)
    assert results == {"egress": "agency-egress:latest"}


def test_build_result_has_elapsed():
    """Built images have non-zero elapsed_seconds."""
    client = _make_client(existing_tags=[])

    with patch("images.build_image"):
        results, _ = build_images_parallel(client)

    for result in results.values():
        assert result.built
        assert result.elapsed_seconds >= 0.0


def test_build_result_cached_zero_elapsed():
    """Cached (existing) images have 0.0 elapsed_seconds."""
    all_tags = [image_tag(c) for c in BUILDABLE]
    client = _make_client(existing_tags=all_tags)

    results, _ = build_images_parallel(client)

    for result in results.values():
        assert not result.built
        assert result.elapsed_seconds == 0.0


def test_any_images_missing_true():
    """any_images_missing returns True when at least one image is missing."""
    client = _make_client(existing_tags=[])
    assert any_images_missing(client) is True


def test_any_images_missing_false():
    """any_images_missing returns False when all images exist."""
    all_tags = [image_tag(c) for c in BUILDABLE]
    client = _make_client(existing_tags=all_tags)
    assert any_images_missing(client) is False


def test_on_progress_callback_called():
    """build_image_streaming parses Step X/Y lines and calls on_progress."""
    client = MagicMock()
    client.api.build.return_value = iter([
        {"stream": "Step 1/3 : FROM python:3.12-slim\n"},
        {"stream": " ---> abc123\n"},
        {"stream": "Step 2/3 : RUN apt-get update\n"},
        {"stream": "Step 3/3 : COPY . /app\n"},
    ])

    progress_calls = []

    with patch("images.IMAGES_DIR", MagicMock()):
        with patch("images.IMAGES_DIR.__truediv__") as mock_div:
            build_dir = MagicMock()
            dockerfile = MagicMock()
            dockerfile.exists.return_value = True
            build_dir.__truediv__ = MagicMock(return_value=dockerfile)
            build_dir.__str__ = MagicMock(return_value="/fake/path")
            mock_div.return_value = build_dir

            build_image_streaming(
                client, "egress",
                on_progress=lambda comp, step, total, desc: progress_calls.append(
                    (comp, step, total)
                ),
            )

    assert len(progress_calls) == 3
    assert progress_calls[0] == ("egress", 1, 3)
    assert progress_calls[1] == ("egress", 2, 3)
    assert progress_calls[2] == ("egress", 3, 3)


def test_streaming_build_error_raises():
    """build_image_streaming raises on error chunks from Docker."""
    client = MagicMock()
    client.api.build.return_value = iter([
        {"stream": "Step 1/2 : FROM python:3.12-slim\n"},
        {"error": "something went wrong"},
    ])

    with patch("images.IMAGES_DIR", MagicMock()):
        with patch("images.IMAGES_DIR.__truediv__") as mock_div:
            build_dir = MagicMock()
            dockerfile = MagicMock()
            dockerfile.exists.return_value = True
            build_dir.__truediv__ = MagicMock(return_value=dockerfile)
            build_dir.__str__ = MagicMock(return_value="/fake/path")
            mock_div.return_value = build_dir

            try:
                build_image_streaming(client, "egress")
                assert False, "Should have raised"
            except RuntimeError as e:
                assert "something went wrong" in str(e)
                assert "egress" in str(e)


def test_parallel_with_on_progress():
    """build_images_parallel uses build_image_streaming when on_progress is provided."""
    client = _make_client(existing_tags=[])

    with patch("images.build_image_streaming") as mock_streaming:
        build_images_parallel(
            client,
            on_progress=lambda comp, step, total, desc: None,
        )

    assert mock_streaming.call_count == len(BUILDABLE)
