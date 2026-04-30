"""Tests for ontology-driven synthesizer: validation, migration, version stamping."""

import json
import os
from pathlib import Path
from unittest.mock import patch, MagicMock

import pytest
import yaml

from services.knowledge.store import KnowledgeStore
from services.knowledge.synthesizer import LLMSynthesizer


SAMPLE_ONTOLOGY = {
    "version": 3,
    "name": "default",
    "description": "Test ontology",
    "entity_types": {
        "person": {"description": "A person", "attributes": ["name"]},
        "system": {"description": "A system", "attributes": ["name"]},
        "decision": {"description": "A decision", "attributes": ["description"]},
        "finding": {"description": "A finding", "attributes": ["description"]},
        "fact": {"description": "A fact", "attributes": ["description"]},
        "concept": {"description": "A concept", "attributes": ["name"]},
        "incident": {"description": "An incident", "attributes": ["title"]},
        "task": {"description": "A task", "attributes": ["title"]},
        "resolution": {"description": "A resolution", "attributes": ["description"]},
        "service": {"description": "A service", "attributes": ["name"]},
    },
    "relationship_types": {
        "owns": {"description": "Has ownership of", "inverse": "owned_by"},
        "depends_on": {"description": "Depends on", "inverse": "depended_on_by"},
        "relates_to": {"description": "Is related to", "inverse": "relates_to"},
        "blocked_by": {"description": "Is blocked by", "inverse": "blocks"},
        "part_of": {"description": "Is part of", "inverse": "contains"},
        "resolved_by": {"description": "Was resolved by", "inverse": "resolved"},
        "managed_by": {"description": "Managed by", "inverse": "manages"},
        "escalate_to": {"description": "Escalate to", "inverse": "escalation_target_for"},
    },
}


@pytest.fixture
def ontology_path(tmp_path):
    """Write a sample ontology and return its path."""
    p = tmp_path / "ontology.yaml"
    p.write_text(yaml.dump(SAMPLE_ONTOLOGY))
    return p


@pytest.fixture
def synth_with_ontology(tmp_path, ontology_path):
    """Create an LLMSynthesizer with an ontology loaded."""
    store = KnowledgeStore(tmp_path / "data")
    with patch.dict(os.environ, {"AGENCY_ONTOLOGY_PATH": str(ontology_path)}):
        synth = LLMSynthesizer(store)
    assert synth._ontology is not None
    return synth


@pytest.fixture
def synth_without_ontology(tmp_path):
    """Create an LLMSynthesizer with no ontology."""
    store = KnowledgeStore(tmp_path / "data")
    with patch.dict(os.environ, {"AGENCY_ONTOLOGY_PATH": str(tmp_path / "nonexistent.yaml")}):
        synth = LLMSynthesizer(store)
    assert synth._ontology is None
    return synth


class TestEntityKindValidation:
    def test_exact_match(self, synth_with_ontology):
        assert synth_with_ontology._validate_kind("person") == "person"
        assert synth_with_ontology._validate_kind("system") == "system"
        assert synth_with_ontology._validate_kind("decision") == "decision"

    def test_case_insensitive(self, synth_with_ontology):
        assert synth_with_ontology._validate_kind("Person") == "person"
        assert synth_with_ontology._validate_kind("SYSTEM") == "system"

    def test_alias_mapping(self, synth_with_ontology):
        assert synth_with_ontology._validate_kind("agent") == "system"
        assert synth_with_ontology._validate_kind("application") == "system"
        assert synth_with_ontology._validate_kind("observation") == "finding"
        assert synth_with_ontology._validate_kind("bug") == "incident"
        assert synth_with_ontology._validate_kind("ticket") == "task"
        assert synth_with_ontology._validate_kind("component") == "system"

    def test_unknown_falls_back_to_fact(self, synth_with_ontology):
        assert synth_with_ontology._validate_kind("totally_unknown_type") == "fact"

    def test_empty_falls_back_to_fact(self, synth_with_ontology):
        assert synth_with_ontology._validate_kind("") == "fact"

    def test_no_ontology_passthrough(self, synth_without_ontology):
        assert synth_without_ontology._validate_kind("whatever") == "whatever"
        assert synth_without_ontology._validate_kind("") == "fact"


class TestRelationshipValidation:
    def test_exact_match(self, synth_with_ontology):
        assert synth_with_ontology._validate_relation("owns") == "owns"
        assert synth_with_ontology._validate_relation("depends_on") == "depends_on"

    def test_inverse_recognized(self, synth_with_ontology):
        assert synth_with_ontology._validate_relation("owned_by") == "owned_by"
        assert synth_with_ontology._validate_relation("blocks") == "blocks"

    def test_alias_mapping(self, synth_with_ontology):
        assert synth_with_ontology._validate_relation("related") == "relates_to"
        assert synth_with_ontology._validate_relation("related_to") == "relates_to"
        assert synth_with_ontology._validate_relation("belongs_to") == "part_of"
        assert synth_with_ontology._validate_relation("fixed_by") == "resolved_by"
        assert synth_with_ontology._validate_relation("requires") == "depends_on"

    def test_unknown_falls_back_to_relates_to(self, synth_with_ontology):
        assert synth_with_ontology._validate_relation("totally_unknown") == "relates_to"

    def test_empty_falls_back_to_relates_to(self, synth_with_ontology):
        assert synth_with_ontology._validate_relation("") == "relates_to"

    def test_no_ontology_passthrough(self, synth_without_ontology):
        assert synth_without_ontology._validate_relation("whatever") == "whatever"
        assert synth_without_ontology._validate_relation("") == "relates_to"


class TestOntologyVersionStamping:
    def test_new_nodes_get_version(self, synth_with_ontology):
        extraction = {
            "entities": [{"label": "nginx", "kind": "system", "summary": "web server"}],
            "relationships": [],
        }
        synth_with_ontology._apply_extraction(extraction, ["#general"])
        nodes = synth_with_ontology.store.find_nodes("nginx")
        assert len(nodes) == 1
        props = json.loads(nodes[0].get("properties", "{}"))
        assert props.get("_ontology_version") == 3

    def test_no_version_without_ontology(self, synth_without_ontology):
        extraction = {
            "entities": [{"label": "nginx", "kind": "system", "summary": "web server"}],
            "relationships": [],
        }
        synth_without_ontology._apply_extraction(extraction, ["#general"])
        nodes = synth_without_ontology.store.find_nodes("nginx")
        assert len(nodes) == 1
        props = json.loads(nodes[0].get("properties", "{}"))
        assert "_ontology_version" not in props


class TestApplyExtractionValidation:
    def test_entities_validated(self, synth_with_ontology):
        """LLM-produced freeform kinds get validated on ingestion."""
        extraction = {
            "entities": [
                {"label": "nginx", "kind": "application", "summary": "web server"},
                {"label": "dns bug", "kind": "bug", "summary": "DNS fails"},
                {"label": "some widget", "kind": "totally_unknown", "summary": "mysterious"},
            ],
            "relationships": [],
        }
        synth_with_ontology._apply_extraction(extraction, ["#general"])
        store = synth_with_ontology.store

        # "application" → "system"
        nodes = store.find_nodes("nginx")
        assert len(nodes) == 1
        assert nodes[0]["kind"] == "system"

        # "bug" → "incident"
        nodes = store.find_nodes("dns bug")
        assert len(nodes) == 1
        assert nodes[0]["kind"] == "incident"

        # unknown → "fact"
        nodes = store.find_nodes("some widget")
        assert len(nodes) == 1
        assert nodes[0]["kind"] == "fact"

    def test_relationships_validated(self, synth_with_ontology):
        """LLM-produced freeform relations get validated on ingestion."""
        extraction = {
            "entities": [
                {"label": "nginx", "kind": "system", "summary": "web"},
                {"label": "redis", "kind": "system", "summary": "cache"},
            ],
            "relationships": [
                {"source": "nginx", "target": "redis", "relation": "requires"},
            ],
        }
        synth_with_ontology._apply_extraction(extraction, ["#general"])
        store = synth_with_ontology.store

        nginx = store.find_nodes("nginx")
        assert len(nginx) == 1
        edges = store.get_edges(nginx[0]["id"], direction="outgoing")
        assert len(edges) == 1
        assert edges[0]["relation"] == "depends_on"  # "requires" → "depends_on"


class TestFreeformMigration:
    def test_migration_remaps_kinds(self, synth_with_ontology):
        store = synth_with_ontology.store
        # Insert nodes with freeform kinds
        store.add_node(label="scan result", kind="observation", summary="found vuln")
        store.add_node(label="server crash", kind="bug", summary="OOM")
        store.add_node(label="redis", kind="fact", summary="cache")  # already valid

        result = synth_with_ontology.migrate_freeform_kinds()

        assert result["total"] == 3
        assert result["remapped"] == 2  # observation→finding, bug→incident
        assert result["unchanged"] == 1  # fact stays

        # Verify remapped
        nodes = store.find_nodes("scan result")
        assert nodes[0]["kind"] == "finding"
        nodes = store.find_nodes("server crash")
        assert nodes[0]["kind"] == "incident"
        nodes = store.find_nodes("redis")
        assert nodes[0]["kind"] == "fact"

    def test_migration_writes_marker(self, synth_with_ontology):
        synth_with_ontology.migrate_freeform_kinds()
        marker = Path(synth_with_ontology.store.data_dir) / ".ontology-migrated"
        assert marker.exists()

    def test_migration_skips_if_already_done(self, synth_with_ontology):
        marker = Path(synth_with_ontology.store.data_dir) / ".ontology-migrated"
        marker.write_text("already done\n")
        result = synth_with_ontology.migrate_freeform_kinds()
        assert result.get("skipped") is True

    def test_migration_no_ontology(self, synth_without_ontology):
        result = synth_without_ontology.migrate_freeform_kinds()
        assert result["total"] == 0

    def test_migration_preserves_original_kind(self, synth_with_ontology):
        store = synth_with_ontology.store
        store.add_node(label="scan result", kind="observation", summary="found vuln")
        synth_with_ontology.migrate_freeform_kinds()
        nodes = store.find_nodes("scan result")
        props = json.loads(nodes[0].get("properties", "{}"))
        assert props.get("_original_kind") == "observation"
        assert props.get("_migrated") is True


class TestOntologyPromptIntegration:
    def test_typed_prompt_used_with_ontology(self, synth_with_ontology):
        prompt = synth_with_ontology._build_extraction_prompt(
            [{"author": "test", "content": "hello", "channel": "general"}]
        )
        assert "Use ONLY these entity types" in prompt
        assert "Use ONLY these relationship types" in prompt
        assert "**person**" in prompt
        assert "**owns**" in prompt

    def test_freeform_prompt_without_ontology(self, synth_without_ontology):
        prompt = synth_without_ontology._build_extraction_prompt(
            [{"author": "test", "content": "hello", "channel": "general"}]
        )
        assert "Use whatever" in prompt
        assert "Use ONLY" not in prompt

    def test_ontology_hot_reload(self, tmp_path, ontology_path):
        store = KnowledgeStore(tmp_path / "data")
        env = {"AGENCY_ONTOLOGY_PATH": str(ontology_path)}
        with patch.dict(os.environ, env):
            synth = LLMSynthesizer(store)
            assert len(synth._ontology["entity_types"]) == 10

            # Modify ontology — add a new type
            import time
            time.sleep(0.05)  # Ensure mtime changes
            updated = SAMPLE_ONTOLOGY.copy()
            updated["entity_types"] = {**SAMPLE_ONTOLOGY["entity_types"], "vulnerability": {"description": "A vuln", "attributes": ["cve"]}}
            ontology_path.write_text(yaml.dump(updated))

            synth._build_extraction_prompt(
                [{"author": "test", "content": "hello", "channel": "general"}]
            )
            assert "vulnerability" in synth._ontology["entity_types"]
