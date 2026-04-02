import json
import sys
from pathlib import Path
from unittest.mock import MagicMock

# Add the body directory to sys.path so body.py can be imported directly,
# matching the pattern used by other body runtime tests.
_body_dir = Path(__file__).parent.parent.parent / "body"
if str(_body_dir) not in sys.path:
    sys.path.insert(0, str(_body_dir))

from body import classify_llm_error  # noqa: E402


def test_classify_401_provider_auth():
    import httpx
    response = MagicMock()
    response.status_code = 401
    error = httpx.HTTPStatusError("401", request=MagicMock(), response=response)
    result = classify_llm_error(error, model="claude-sonnet", correlation_id="bot-1", retries=3)
    assert result["category"] == "llm.call_failed"
    assert result["stage"] == "provider_auth"
    assert result["status"] == 401
    assert result["model"] == "claude-sonnet"
    assert result["retries_attempted"] == 3


def test_classify_429_rate_limit():
    import httpx
    response = MagicMock()
    response.status_code = 429
    error = httpx.HTTPStatusError("429", request=MagicMock(), response=response)
    result = classify_llm_error(error, model="claude-sonnet")
    assert result["stage"] == "provider_rate_limit"
    assert result["status"] == 429


def test_classify_timeout():
    import httpx
    result = classify_llm_error(httpx.ReadTimeout("timeout"), model="claude-sonnet")
    assert result["stage"] == "timeout"
    assert result["status"] is None


def test_classify_connection_error():
    result = classify_llm_error(ConnectionError("refused"), model="claude-sonnet")
    assert result["stage"] == "proxy_unreachable"


def test_classify_json_error():
    result = classify_llm_error(json.JSONDecodeError("bad", "", 0), model="claude-sonnet")
    assert result["stage"] == "response_malformed"


def test_classify_500_provider_error():
    import httpx
    response = MagicMock()
    response.status_code = 502
    error = httpx.HTTPStatusError("502", request=MagicMock(), response=response)
    result = classify_llm_error(error)
    assert result["stage"] == "provider_error"
    assert result["status"] == 502
