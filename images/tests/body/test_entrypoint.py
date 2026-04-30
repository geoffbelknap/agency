from pathlib import Path


def test_body_entrypoint_exports_enforcer_health_url_for_python_poll():
    images_dir = Path(__file__).resolve().parents[2]
    entrypoint = (images_dir / "body" / "entrypoint.sh").read_text()

    assert "export ENFORCER_HEALTH_URL=" in entrypoint
    assert "os.environ['ENFORCER_HEALTH_URL']" in entrypoint
