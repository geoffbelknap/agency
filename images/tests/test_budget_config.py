"""Tests for BudgetConfig and NotifyConfig models."""

import pytest
from images.models.constraints import BudgetConfig, NotifyConfig


class TestBudgetConfigDefaults:
    def test_default_mode_is_notify(self):
        b = BudgetConfig()
        assert b.mode == "notify"

    def test_default_limits_are_zero(self):
        b = BudgetConfig()
        assert b.soft_limit == 0.0
        assert b.hard_limit == 0.0
        assert b.max_daily_usd == 0.0
        assert b.max_session_usd == 0.0
        assert b.max_total_usd == 0.0

    def test_default_warning_threshold(self):
        b = BudgetConfig()
        assert b.warning_threshold_pct == 80

    def test_default_notify_config(self):
        b = BudgetConfig()
        assert b.notify.webhook is None
        assert b.notify.email is None
        assert b.notify.log is True


class TestBudgetConfigModes:
    def test_hard_mode(self):
        b = BudgetConfig(mode="hard", hard_limit=50.0)
        assert b.mode == "hard"
        assert b.hard_limit == 50.0

    def test_soft_mode_with_limits(self):
        b = BudgetConfig(mode="soft", soft_limit=10.0, hard_limit=50.0)
        assert b.mode == "soft"
        assert b.soft_limit == 10.0
        assert b.hard_limit == 50.0

    def test_notify_mode(self):
        b = BudgetConfig(mode="notify", soft_limit=5.0)
        assert b.mode == "notify"
        assert b.soft_limit == 5.0

    def test_invalid_mode_rejected(self):
        with pytest.raises(Exception):
            BudgetConfig(mode="invalid")


class TestNotifyConfig:
    def test_webhook_and_email(self):
        n = NotifyConfig(webhook="https://hooks.example.com/budget", email="ops@example.com")
        assert n.webhook == "https://hooks.example.com/budget"
        assert n.email == "ops@example.com"
        assert n.log is True

    def test_log_only(self):
        n = NotifyConfig(log=True)
        assert n.webhook is None
        assert n.email is None

    def test_disable_log(self):
        n = NotifyConfig(log=False)
        assert n.log is False

    def test_extra_fields_forbidden(self):
        with pytest.raises(Exception):
            NotifyConfig(slack_channel="#alerts")

    def test_budget_with_notify_config(self):
        b = BudgetConfig(
            mode="soft",
            soft_limit=10.0,
            hard_limit=50.0,
            notify=NotifyConfig(
                webhook="https://hooks.example.com/budget",
                email="ops@example.com",
            ),
        )
        assert b.notify.webhook == "https://hooks.example.com/budget"
        assert b.notify.email == "ops@example.com"


class TestBudgetConfigValidation:
    def test_extra_fields_forbidden(self):
        with pytest.raises(Exception):
            BudgetConfig(unknown_field="bad")

    def test_negative_soft_limit_rejected(self):
        with pytest.raises(Exception):
            BudgetConfig(soft_limit=-1.0)

    def test_negative_hard_limit_rejected(self):
        with pytest.raises(Exception):
            BudgetConfig(hard_limit=-5.0)

    def test_negative_daily_rejected(self):
        with pytest.raises(Exception):
            BudgetConfig(max_daily_usd=-1.0)

    def test_negative_session_rejected(self):
        with pytest.raises(Exception):
            BudgetConfig(max_session_usd=-0.5)

    def test_warning_pct_zero_rejected(self):
        with pytest.raises(Exception):
            BudgetConfig(warning_threshold_pct=0)

    def test_warning_pct_over_100_rejected(self):
        with pytest.raises(Exception):
            BudgetConfig(warning_threshold_pct=101)


class TestBudgetConfigBackwardCompat:
    def test_existing_fields_still_work(self):
        b = BudgetConfig(
            max_daily_usd=10.0,
            max_session_usd=5.0,
            max_total_usd=100.0,
            warning_threshold_pct=90,
        )
        assert b.max_daily_usd == 10.0
        assert b.max_session_usd == 5.0
        assert b.max_total_usd == 100.0
        assert b.warning_threshold_pct == 90

    def test_existing_fields_with_new_mode(self):
        b = BudgetConfig(
            mode="hard",
            max_daily_usd=10.0,
            hard_limit=50.0,
        )
        assert b.mode == "hard"
        assert b.max_daily_usd == 10.0
        assert b.hard_limit == 50.0
