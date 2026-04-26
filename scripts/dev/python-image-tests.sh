#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

PYTEST_BIN="${PYTEST_BIN:-.venv/bin/python -m pytest}"
COMMON_ARGS=(-x -vv --timeout=30 --durations=20)

usage() {
  cat <<'EOF'
Usage: scripts/dev/python-image-tests.sh [lane...]

Runs Python image/body tests in bounded lanes instead of one giant pytest
process. This avoids local file-descriptor/resource exhaustion while preserving
the important validation surfaces.

Lanes:
  core             Aggregate for the bounded non-body/non-realtime lanes below
  core-foundation  Audit, policy, validation, extraction, and common models
  core-comms       Communication, subscriptions, websocket, and Slack bridge tests
  core-runtime     Connector, intake, routing, gateway, and service tooling tests
  core-graph       Knowledge graph, memory, scope, ontology, and work item tests
  knowledge        Heavy knowledge/synthesis tests split out from core
  realtime         Realtime comms E2E tests
  body             Body runtime tests
  all              Run all lanes (default)
EOF
}

run_pytest() {
  local name="$1"
  shift
  echo "==> Python lane: ${name}"
  # shellcheck disable=SC2086
  $PYTEST_BIN "$@" "${COMMON_ARGS[@]}"
}

run_core_foundation() {
  run_pytest core-foundation \
    images/tests/audit/ \
    images/tests/test_asset_ontology.py \
    images/tests/test_audit_retention.py \
    images/tests/test_authority_tools.py \
    images/tests/test_budget_config.py \
    images/tests/test_classification.py \
    images/tests/test_code_extractor.py \
    images/tests/test_correlation.py \
    images/tests/test_credential_swap.py \
    images/tests/test_embedding_providers.py \
    images/tests/test_event_id_threading.py \
    images/tests/test_exception_recommend.py \
    images/tests/test_exception_routing.py \
    images/tests/test_extractors.py \
    images/tests/test_host_model.py \
    images/tests/test_html_extractor.py \
    images/tests/test_key_resolver.py \
    images/tests/test_nested_match.py \
    images/tests/test_pack.py \
    images/tests/test_pact_pre_commit_rewire.py \
    images/tests/test_pact_trajectories.py \
    images/tests/test_pdf_extractor.py \
    images/tests/test_policy.py \
    images/tests/test_principal_registry.py \
    images/tests/test_quarantine.py \
    images/tests/test_source_classifier.py \
    images/tests/test_validation.py \
    images/tests/test_xpia_scan.py
}

run_core_comms() {
  run_pytest core-comms \
    images/tests/test_bridge_state.py \
    images/tests/test_channel_watcher.py \
    images/tests/test_channel_watch_payloads.py \
    images/tests/test_comms_context.py \
    images/tests/test_comms_delivery.py \
    images/tests/test_comms_federation.py \
    images/tests/test_comms_matcher.py \
    images/tests/test_comms_models.py \
    images/tests/test_comms_server.py \
    images/tests/test_comms_store.py \
    images/tests/test_comms_subscriptions.py \
    images/tests/test_comms_subscriptions_api.py \
    images/tests/test_comms_tools.py \
    images/tests/test_comms_websocket.py \
    images/tests/test_slack_bridge_v1.py \
    images/tests/test_subscription_models.py
}

run_core_runtime() {
  run_pytest core-runtime \
    images/tests/test_connector.py \
    images/tests/test_connector_requirements.py \
    images/tests/test_connector_routing.py \
    images/tests/test_gateway_client.py \
    images/tests/test_image_builds.py \
    images/tests/test_image_profiling.py \
    images/tests/test_ingestion_pipeline.py \
    images/tests/test_intake_route_rendering.py \
    images/tests/test_intake_server.py \
    images/tests/test_intake_sources.py \
    images/tests/test_poll_cron.py \
    images/tests/test_poller.py \
    images/tests/test_routing.py \
    images/tests/test_scheduler.py \
    images/tests/test_service_tools.py \
    images/tests/test_swap_handlers.py \
    images/tests/test_watcher.py
}

run_core_graph() {
  run_pytest core-graph \
    images/tests/test_edge_provenance.py \
    images/tests/test_graph_ingest.py \
    images/tests/test_knowledge_ingester.py \
    images/tests/test_knowledge_push.py \
    images/tests/test_knowledge_store.py \
    images/tests/test_knowledge_store_init.py \
    images/tests/test_knowledge_tools.py \
    images/tests/test_memory_manager.py \
    images/tests/test_merge_buffer.py \
    images/tests/test_ontology_emergence.py \
    images/tests/test_query_graph.py \
    images/tests/test_relationship_inference.py \
    images/tests/test_save_insight.py \
    images/tests/test_scope_model.py \
    images/tests/test_synthesizer_ontology.py \
    images/tests/test_work_contract.py \
    images/tests/test_work_items.py
}

run_core() {
  run_core_foundation
  run_core_comms
  run_core_runtime
  run_core_graph
}

run_knowledge() {
  run_pytest knowledge \
    images/tests/test_graph_intelligence.py \
    images/tests/test_knowledge_curator.py \
    images/tests/test_knowledge_federation.py \
    images/tests/test_knowledge_org_context.py \
    images/tests/test_knowledge_review_gate.py \
    images/tests/test_knowledge_server.py \
    images/tests/test_knowledge_synthesizer.py
}

run_realtime() {
  run_pytest realtime \
    images/tests/test_realtime_comms_e2e.py \
    images/tests/test_comms_e2e.py
}

run_body() {
  run_pytest body \
    images/body/ \
    images/tests/body/ \
    images/tests/test_body_interruption.py \
    images/tests/test_body_provider_tools.py \
    images/tests/test_body_realtime.py \
    images/tests/test_body_ws_listener.py
}

lanes=("$@")
if [[ ${#lanes[@]} -eq 0 ]]; then
  lanes=(all)
fi

for lane in "${lanes[@]}"; do
  case "$lane" in
    all)
      run_core
      run_knowledge
      run_realtime
      run_body
      ;;
    core) run_core ;;
    core-foundation) run_core_foundation ;;
    core-comms) run_core_comms ;;
    core-runtime) run_core_runtime ;;
    core-graph) run_core_graph ;;
    knowledge) run_knowledge ;;
    realtime) run_realtime ;;
    body) run_body ;;
    -h|--help|help)
      usage
      exit 0
      ;;
    *)
      echo "unknown lane: $lane" >&2
      usage >&2
      exit 2
      ;;
  esac
done
