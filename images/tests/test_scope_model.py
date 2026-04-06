"""Tests for the Scope authorization model."""

import json

import pytest

from knowledge.scope import Scope


class TestScopeCreation:
    def test_defaults(self):
        s = Scope()
        assert s.channels == []
        assert s.principals == []
        assert s.classification is None

    def test_with_values(self):
        s = Scope(channels=["#ops"], principals=["alice"], classification="internal")
        assert s.channels == ["#ops"]
        assert s.principals == ["alice"]
        assert s.classification == "internal"


class TestToDict:
    def test_without_classification(self):
        s = Scope(channels=["b", "a"], principals=["z", "m"])
        d = s.to_dict()
        assert d == {"channels": ["a", "b"], "principals": ["m", "z"]}
        assert "classification" not in d

    def test_with_classification(self):
        s = Scope(channels=["c1"], classification="secret")
        d = s.to_dict()
        assert d["classification"] == "secret"

    def test_empty_scope(self):
        d = Scope().to_dict()
        assert d == {"channels": [], "principals": []}


class TestFromDict:
    def test_full_roundtrip(self):
        original = Scope(channels=["#sec", "#ops"], principals=["bob"], classification="internal")
        restored = Scope.from_dict(original.to_dict())
        assert restored.channels == sorted(original.channels)
        assert restored.principals == original.principals
        assert restored.classification == original.classification

    def test_json_roundtrip(self):
        original = Scope(channels=["z", "a"], principals=["p1"], classification="top")
        restored = Scope.from_dict(json.loads(json.dumps(original.to_dict())))
        assert restored.to_dict() == original.to_dict()

    def test_missing_fields(self):
        s = Scope.from_dict({})
        assert s.channels == []
        assert s.principals == []
        assert s.classification is None

    def test_partial_dict(self):
        s = Scope.from_dict({"channels": ["#dm"]})
        assert s.channels == ["#dm"]
        assert s.principals == []


class TestFromSourceChannels:
    def test_creates_scope_from_channels(self):
        s = Scope.from_source_channels(["#alerts", "#general"])
        assert s.channels == ["#alerts", "#general"]
        assert s.principals == []
        assert s.classification is None

    def test_copies_list(self):
        original = ["#a"]
        s = Scope.from_source_channels(original)
        original.append("#b")
        assert s.channels == ["#a"]


class TestOverlaps:
    def test_empty_overlaps_everything(self):
        empty = Scope()
        nonempty = Scope(channels=["#ops"], principals=["alice"])
        assert empty.overlaps(nonempty)
        assert nonempty.overlaps(empty)

    def test_both_empty_overlap(self):
        assert Scope().overlaps(Scope())

    def test_channel_overlap(self):
        a = Scope(channels=["#ops", "#sec"])
        b = Scope(channels=["#sec", "#dev"])
        assert a.overlaps(b)

    def test_principal_overlap(self):
        a = Scope(principals=["alice", "bob"])
        b = Scope(principals=["bob", "carol"])
        assert a.overlaps(b)

    def test_no_overlap(self):
        a = Scope(channels=["#ops"], principals=["alice"])
        b = Scope(channels=["#dev"], principals=["bob"])
        assert not a.overlaps(b)

    def test_channel_overlap_only(self):
        a = Scope(channels=["#ops"], principals=["alice"])
        b = Scope(channels=["#ops"], principals=["bob"])
        assert a.overlaps(b)

    def test_principal_overlap_only(self):
        a = Scope(channels=["#ops"], principals=["alice"])
        b = Scope(channels=["#dev"], principals=["alice"])
        assert a.overlaps(b)

    def test_empty_channels_nonempty_principals_vs_nonempty_channels(self):
        """One scope has only principals, other has only channels -- no overlap."""
        a = Scope(principals=["alice"])
        b = Scope(channels=["#ops"])
        assert not a.overlaps(b)


class TestIntersection:
    def test_partial_overlap(self):
        a = Scope(channels=["#ops", "#sec"], principals=["alice", "bob"])
        b = Scope(channels=["#sec", "#dev"], principals=["bob", "carol"])
        result = a.intersection(b)
        assert result.channels == ["#sec"]
        assert result.principals == ["bob"]

    def test_no_overlap(self):
        a = Scope(channels=["#ops"], principals=["alice"])
        b = Scope(channels=["#dev"], principals=["bob"])
        result = a.intersection(b)
        assert result.channels == []
        assert result.principals == []

    def test_result_is_sorted(self):
        a = Scope(channels=["z", "a", "m"], principals=["z", "a"])
        b = Scope(channels=["m", "z"], principals=["a", "z"])
        result = a.intersection(b)
        assert result.channels == ["m", "z"]
        assert result.principals == ["a", "z"]

    def test_classification_not_carried(self):
        a = Scope(channels=["#x"], classification="secret")
        b = Scope(channels=["#x"], classification="public")
        result = a.intersection(b)
        assert result.classification is None


class TestIsNarrowerThan:
    def test_subset(self):
        narrow = Scope(channels=["#ops"], principals=["alice"])
        wide = Scope(channels=["#ops", "#sec"], principals=["alice", "bob"])
        assert narrow.is_narrower_than(wide)

    def test_equal_scopes(self):
        a = Scope(channels=["#ops"], principals=["alice"])
        b = Scope(channels=["#ops"], principals=["alice"])
        assert a.is_narrower_than(b)

    def test_not_narrower(self):
        wide = Scope(channels=["#ops", "#sec"], principals=["alice", "bob"])
        narrow = Scope(channels=["#ops"], principals=["alice"])
        assert not wide.is_narrower_than(narrow)

    def test_empty_is_narrower_than_anything(self):
        assert Scope().is_narrower_than(Scope(channels=["#ops"]))

    def test_empty_is_narrower_than_empty(self):
        assert Scope().is_narrower_than(Scope())

    def test_partial_subset_fails(self):
        """Channels are subset but principals are not."""
        a = Scope(channels=["#ops"], principals=["alice", "bob"])
        b = Scope(channels=["#ops", "#sec"], principals=["alice"])
        assert not a.is_narrower_than(b)
