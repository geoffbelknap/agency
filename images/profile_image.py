"""Build-time image profiling -- measure baseline RSS and label images."""

import os
import tempfile
import time

import docker


def profile_image_rss(client: docker.DockerClient, image_tag: str, timeout: int = 10) -> int:
    """Run a container from image_tag, wait briefly, read RSS from cgroup, return MB.

    Returns 0 if profiling fails (image won't start, no cgroup info, etc).
    """
    container = None
    try:
        container = client.containers.run(
            image=image_tag,
            detach=True,
            mem_limit="1g",
            remove=False,
        )
        # Wait for container to initialize
        time.sleep(min(timeout, 3))

        # Read memory usage from cgroup v2 (or v1 fallback)
        stats = container.stats(stream=False)
        mem_usage = stats.get("memory_stats", {}).get("usage", 0)
        rss_mb = mem_usage // (1024 * 1024)
        return max(1, rss_mb) if rss_mb > 0 else 0
    except Exception:
        return 0
    finally:
        if container:
            try:
                container.stop(timeout=5)
                container.remove(force=True)
            except Exception:
                pass


def label_image(client: docker.DockerClient, image_tag: str, labels: dict[str, str]) -> None:
    """Add labels to an existing image by re-tagging with a minimal Dockerfile.

    Docker doesn't support adding labels to existing images directly,
    so we build a one-line Dockerfile: FROM image_tag with LABEL directives.
    """
    label_args = " ".join(f'{k}="{v}"' for k, v in labels.items())
    dockerfile_content = f"FROM {image_tag}\nLABEL {label_args}\n"

    with tempfile.TemporaryDirectory() as tmpdir:
        dockerfile = os.path.join(tmpdir, "Dockerfile")
        with open(dockerfile, "w") as f:
            f.write(dockerfile_content)
        client.images.build(path=tmpdir, tag=image_tag, rm=True, quiet=True)


def profile_and_label(client: docker.DockerClient, image_tag: str, timeout: int = 10) -> int:
    """Profile an image's RSS and label it with agency.baseline_rss_mb.

    Returns the measured RSS in MB, or 0 if profiling failed.
    """
    rss_mb = profile_image_rss(client, image_tag, timeout=timeout)
    if rss_mb > 0:
        label_image(client, image_tag, {"agency.baseline_rss_mb": str(rss_mb)})
    return rss_mb
