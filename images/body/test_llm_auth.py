from pathlib import Path


def test_llm_api_key_prefers_environment(monkeypatch, tmp_path):
    from body import Body

    body = Body.__new__(Body)
    body.state_dir = Path(tmp_path)
    monkeypatch.setenv("AGENCY_LLM_API_KEY", "agency-scoped-env")

    assert body._llm_api_key() == "agency-scoped-env"


def test_llm_api_key_reads_scoped_key_file(monkeypatch, tmp_path):
    from body import Body

    monkeypatch.delenv("AGENCY_LLM_API_KEY", raising=False)
    auth_dir = Path(tmp_path) / "enforcer-auth"
    auth_dir.mkdir()
    (auth_dir / "api_keys.yaml").write_text(
        '- key: "agency-scoped-file"\n  name: "agency-workspace"\n'
    )
    body = Body.__new__(Body)
    body.state_dir = Path(tmp_path)

    assert body._llm_api_key() == "agency-scoped-file"


def test_llm_auth_headers_fail_closed_without_scoped_key(monkeypatch, tmp_path):
    from body import Body

    monkeypatch.delenv("AGENCY_LLM_API_KEY", raising=False)
    body = Body.__new__(Body)
    body.state_dir = Path(tmp_path)

    try:
        body._llm_auth_headers()
    except RuntimeError as exc:
        assert "refusing unauthenticated" in str(exc)
    else:
        raise AssertionError("expected missing scoped key to fail closed")
