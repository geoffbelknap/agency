"""Docker image building for Agency infrastructure components."""

import re
import shutil
import tempfile
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from contextlib import contextmanager
from dataclasses import dataclass
from pathlib import Path
from typing import Optional, Callable, Iterator

import docker

# Matches Docker build output like "Step 3/10 : RUN apt-get update"
_STEP_RE = re.compile(r"^Step (\d+)/(\d+)\s*:\s*(.*)")

IMAGES_DIR = Path(__file__).parent

# Shared service images: run as long-lived containers on the mediation network
SHARED_IMAGES = ["egress", "comms", "knowledge", "intake"]

# Per-agent images: built once, used when starting each agent (not long-lived containers)
AGENT_IMAGES = ["enforcer", "workspace", "body"]

# All standard infra images: built during `agency infra up`
INFRA_IMAGES = SHARED_IMAGES + AGENT_IMAGES

# All buildable images (for `agency infra build --all`)
BUILDABLE = INFRA_IMAGES

# Optional images: build failures warn instead of error.
OPTIONAL_IMAGES: set[str] = set()


@dataclass
class BuildResult:
    """Result of building a single component image."""

    component: str
    tag: str
    built: bool  # True if actually built, False if already existed
    elapsed_seconds: float = 0.0


def image_tag(component: str) -> str:
    """Return the Docker image tag for a component."""
    return f"agency-{component}:latest"


def _image_exists(client: docker.DockerClient, tag: str) -> bool:
    """Check if an image already exists locally."""
    try:
        client.images.get(tag)
        return True
    except docker.errors.ImageNotFound:
        return False


def any_images_missing(client: docker.DockerClient, components: Optional[list[str]] = None) -> bool:
    """Return True if any component image needs building."""
    for comp in (components or BUILDABLE):
        if not _image_exists(client, image_tag(comp)):
            return True
    return False


@contextmanager
def _resolve_build_context(component: str) -> Iterator[Path]:
    """Yield the build context directory for a component.

    Most components use their image directory directly. The comms
    component needs agency/models/ copied into the context.
    """
    build_dir = IMAGES_DIR / component

    # Components that import from images.models need the model files
    # copied into the build context (the Dockerfile context is the
    # image directory, which can't reach ../../models/).
    models_needed: dict[str, list[str]] = {
        "comms": ["comms.py", "subscriptions.py"],
        "intake": ["connector.py"],
    }

    if component in models_needed:
        tmpdir = tempfile.mkdtemp(prefix=f"agency-{component}-build-")
        try:
            tmp = Path(tmpdir)
            for f in build_dir.iterdir():
                if f.is_file():
                    shutil.copy2(f, tmp / f.name)
            models_dst = tmp / "models"
            models_dst.mkdir()
            (models_dst / "__init__.py").write_text("")
            for model_file in models_needed[component]:
                shutil.copy2(IMAGES_DIR / "models" / model_file, models_dst / model_file)
            yield tmp
        finally:
            shutil.rmtree(tmpdir, ignore_errors=True)
        return

    yield build_dir


def build_image(
    client: docker.DockerClient, component: str, quiet: bool = False, profile: bool = False,
) -> str:
    """Build a single component image from its Dockerfile.

    Args:
        client: Container image client instance.
        component: Component name (e.g. "egress").
        quiet: Suppress build output.
        profile: If True, measure baseline RSS and label the image after build.

    Returns the image tag.
    """
    build_dir = IMAGES_DIR / component
    dockerfile = build_dir / "Dockerfile"
    if not dockerfile.exists():
        raise FileNotFoundError(f"No Dockerfile at {dockerfile}")

    tag = image_tag(component)
    with _resolve_build_context(component) as ctx:
        client.images.build(
            path=str(ctx),
            tag=tag,
            rm=True,
            quiet=quiet,
        )
    if profile:
        from images.profile_image import profile_and_label

        profile_and_label(client, tag)
    return tag


def build_image_streaming(
    client: docker.DockerClient,
    component: str,
    on_progress: Optional[Callable[[str, int, int, str], None]] = None,
    profile: bool = False,
) -> str:
    """Build a single component image with real-time step progress.

    Uses the low-level Docker API to stream build output and parse
    Dockerfile step progress (Step X/Y).

    Args:
        client: Container image client instance.
        component: Component name (e.g. "egress").
        on_progress: Called with (component, step, total, description) for
            each Dockerfile step parsed from the build stream.
        profile: If True, measure baseline RSS and label the image after build.

    Returns:
        The image tag.

    Raises:
        RuntimeError: If the build stream contains an error.
    """
    build_dir = IMAGES_DIR / component
    dockerfile = build_dir / "Dockerfile"
    if not dockerfile.exists():
        raise FileNotFoundError(f"No Dockerfile at {dockerfile}")

    tag = image_tag(component)
    with _resolve_build_context(component) as ctx:
        stream = client.api.build(path=str(ctx), tag=tag, rm=True, decode=True)
        for chunk in stream:
            if "error" in chunk:
                raise RuntimeError(f"Build error ({component}): {chunk['error'].strip()}")
            line = chunk.get("stream", "")
            m = _STEP_RE.match(line.strip())
            if m and on_progress:
                step = int(m.group(1))
                total = int(m.group(2))
                desc = m.group(3).strip()
                on_progress(component, step, total, desc)
    if profile:
        from images.profile_image import profile_and_label

        profile_and_label(client, tag)
    return tag


def build_images_parallel(
    client: docker.DockerClient,
    components: Optional[list[str]] = None,
    force: bool = False,
    on_start: Optional[Callable[[str], None]] = None,
    on_complete: Optional[Callable[[BuildResult], None]] = None,
    on_progress: Optional[Callable[[str, int, int, str], None]] = None,
    max_workers: int = 3,
    profile: bool = False,
) -> tuple[dict[str, BuildResult], list[str]]:
    """Build multiple component images in parallel.

    Args:
        client: Container image client instance.
        components: List of components to build. Defaults to all BUILDABLE.
        force: Rebuild even if images already exist.
        on_start: Called when a component build begins.
        on_complete: Called when a component build finishes.
        on_progress: Called with (component, step, total, description) for
            each Dockerfile step. When provided, uses streaming builds.
        max_workers: Maximum concurrent builds.
        profile: If True, measure baseline RSS and label images after build.

    Returns:
        Tuple of (results dict, warnings list). Results maps component name
        to BuildResult. Warnings contains messages for optional image build
        failures.

    Raises:
        AgencyError: If any required image builds failed (errors aggregated).
    """
    from images.exceptions import AgencyError

    if components is None:
        components = list(BUILDABLE)

    results: dict[str, BuildResult] = {}
    errors: list[str] = []
    warnings: list[str] = []

    def _build_one(component: str) -> BuildResult:
        tag = image_tag(component)
        if not force and _image_exists(client, tag):
            return BuildResult(component=component, tag=tag, built=False)
        if on_start:
            on_start(component)
        t0 = time.monotonic()
        if on_progress:
            build_image_streaming(client, component, on_progress=on_progress, profile=profile)
        else:
            build_image(client, component, quiet=True, profile=profile)
        elapsed = time.monotonic() - t0
        return BuildResult(component=component, tag=tag, built=True, elapsed_seconds=elapsed)

    with ThreadPoolExecutor(max_workers=max_workers) as executor:
        futures = {
            executor.submit(_build_one, comp): comp for comp in components
        }
        for future in as_completed(futures):
            comp = futures[future]
            try:
                result = future.result()
                results[comp] = result
                if on_complete:
                    on_complete(result)
            except Exception as e:
                if comp in OPTIONAL_IMAGES:
                    warnings.append(f"{comp}: {e}")
                else:
                    errors.append(f"{comp}: {e}")

    if errors:
        raise AgencyError(
            f"Image build failed for {len(errors)} component(s):\n"
            + "\n".join(f"  - {err}" for err in errors)
        )

    return results, warnings


def ensure_images(
    client: docker.DockerClient,
    components: Optional[list[str]] = None,
    force: bool = False,
) -> dict[str, str]:
    """Build any missing component images. Returns {component: tag} for all."""
    results, _warnings = build_images_parallel(
        client, components=components, force=force
    )
    return {comp: r.tag for comp, r in results.items()}
