"""Tests for the subscription manager."""

import pytest
from services.comms.subscriptions import SubscriptionManager
from images.models.subscriptions import InterestDeclaration


@pytest.fixture
def manager(tmp_path):
    return SubscriptionManager(data_dir=tmp_path)


class TestRegistration:
    def test_register_interests(self, manager):
        decl = InterestDeclaration(
            task_id="t1",
            description="test",
            keywords=["payments", "latency"],
        )
        manager.register("agent-bob", decl)
        result = manager.get("agent-bob")
        assert result is not None
        assert result.task_id == "t1"

    def test_clear_interests(self, manager):
        decl = InterestDeclaration(task_id="t1", description="test", keywords=["pay"])
        manager.register("agent-bob", decl)
        manager.clear("agent-bob")
        assert manager.get("agent-bob") is None

    def test_overwrite_on_reregister(self, manager):
        d1 = InterestDeclaration(task_id="t1", description="first", keywords=["aaa"])
        d2 = InterestDeclaration(task_id="t2", description="second", keywords=["bbb"])
        manager.register("agent-bob", d1)
        manager.register("agent-bob", d2)
        result = manager.get("agent-bob")
        assert result.task_id == "t2"

    def test_get_nonexistent_returns_none(self, manager):
        assert manager.get("nobody") is None


class TestPersistence:
    def test_survives_restart(self, tmp_path):
        m1 = SubscriptionManager(data_dir=tmp_path)
        decl = InterestDeclaration(
            task_id="t1", description="test", keywords=["payments"]
        )
        m1.register("agent-bob", decl)

        m2 = SubscriptionManager(data_dir=tmp_path)
        result = m2.get("agent-bob")
        assert result is not None
        assert result.task_id == "t1"

    def test_clear_persists(self, tmp_path):
        m1 = SubscriptionManager(data_dir=tmp_path)
        decl = InterestDeclaration(task_id="t1", description="test", keywords=["pay"])
        m1.register("agent-bob", decl)
        m1.clear("agent-bob")

        m2 = SubscriptionManager(data_dir=tmp_path)
        assert m2.get("agent-bob") is None


class TestAuditLog:
    def test_register_logged(self, manager, tmp_path):
        decl = InterestDeclaration(task_id="t1", description="test", keywords=["pay"])
        manager.register("agent-bob", decl)
        log_file = tmp_path / "subscriptions.log"
        assert log_file.exists()
        content = log_file.read_text()
        assert "register" in content
        assert "agent-bob" in content

    def test_clear_logged(self, manager, tmp_path):
        decl = InterestDeclaration(task_id="t1", description="test", keywords=["pay"])
        manager.register("agent-bob", decl)
        manager.clear("agent-bob")
        content = (tmp_path / "subscriptions.log").read_text()
        assert "clear" in content
