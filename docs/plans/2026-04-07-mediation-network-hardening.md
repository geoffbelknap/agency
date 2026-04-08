# Mediation Network Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the flat `agency-mediation` network with a hub-and-spoke topology through the gateway, eliminating direct inter-service communication and reducing blast radius of enforcer compromise.

**Architecture:** Route all inter-service calls (intake→knowledge, intake→comms, knowledge→comms) through the gateway instead of direct HTTP. Then simplify the network topology: `agency-gateway` (hub for all services and enforcers), `agency-egress-int` (services that need outbound proxy), `agency-egress-ext` (egress→internet). Remove knowledge from agent-internal networks.

**Tech Stack:** Go (gateway, enforcer, infra orchestration), Python (intake, knowledge services), Docker networking

**Spec:** `docs/specs/mediation-network-hardening.md`

---

## File Map

### Phase 1: Decouple Inter-Service Communication

| Action | File | Responsibility |
|--------|------|---------------|
| Modify | `internal/api/routes.go:57-100` | Add graph and event-publish routes to socket router |
| Create | `internal/api/events/handlers_publish.go` | `POST /api/v1/events/publish` — internal event publishing endpoint |
| Create | `internal/api/events/handlers_publish_test.go` | Tests for publish endpoint |
| Modify | `images/intake/server.py` | Replace direct comms/knowledge HTTP calls with gateway-routed calls |
| Create | `images/intake/gateway_client.py` | Gateway HTTP client for intake (graph ingest, event publish) |
| Create | `images/tests/test_gateway_client.py` | Tests for gateway client |
| Modify | `images/knowledge/server.py:68-78,169,635` | Replace direct comms HTTP calls with gateway event publish |
| Create | `images/knowledge/gateway_client.py` | Gateway HTTP client for knowledge (event publish) |
| Modify | `internal/orchestrate/infra.go:806-810` | Remove `KNOWLEDGE_URL` and `COMMS_URL` env vars from intake |
| Modify | `internal/orchestrate/infra.go:722-724` | Update knowledge `NO_PROXY` to remove cross-service hostnames |
| Modify | `internal/orchestrate/infra.go:881-883` | Update web-fetch `NO_PROXY` to remove cross-service hostnames |

### Phase 2: Network Topology Swap

| Action | File | Responsibility |
|--------|------|---------------|
| Modify | `internal/orchestrate/infra.go:33-39` | Replace network constants |
| Modify | `internal/orchestrate/infra.go:463-499` | `ensureNetworks` — create new networks |
| Modify | `internal/orchestrate/infra.go:503-553` | `ensureGatewayProxy` — use `agency-gateway` network, remove ExtraHosts |
| Modify | `internal/orchestrate/infra.go:555-645` | `ensureEgress` — primary on `agency-egress-int`, connect to `agency-gateway` + `agency-egress-ext`, remove ExtraHosts |
| Modify | `internal/orchestrate/infra.go:648-706` | `ensureComms` — use `agency-gateway` |
| Modify | `internal/orchestrate/infra.go:708-778` | `ensureKnowledge` — use `agency-gateway`, connect to `agency-egress-int`, remove `connectToAgentNetworks` |
| Modify | `internal/orchestrate/infra.go:780-857` | `ensureIntake` — use `agency-gateway`, connect to `agency-egress-int` |
| Modify | `internal/orchestrate/infra.go:859-924` | `ensureWebFetch` — use `agency-gateway`, connect to `agency-egress-int` |
| Modify | `internal/orchestrate/infra.go:1040-1099` | `ensureEmbeddings` — use `agency-gateway` |
| Modify | `internal/orchestrate/infra.go:1320-1341` | Delete `connectToAgentNetworks` function |
| Modify | `internal/orchestrate/enforcer.go:225-228` | Connect enforcer to `agency-gateway` + `agency-egress-int` instead of `agency-mediation` |
| Modify | `internal/orchestrate/containers/networks.go` | Add `CreateMediationNetwork` factory (internal, no external route) |
| Create | `internal/orchestrate/infra_networks_test.go` | Tests for new network topology |

### Phase 3: Socket Hardening

| Action | File | Responsibility |
|--------|------|---------------|
| Create | `internal/api/middleware_caller.go` | `X-Agency-Caller` validation middleware |
| Create | `internal/api/middleware_caller_test.go` | Tests for caller middleware |
| Modify | `internal/api/routes.go:57-100` | Apply caller middleware to socket routes |
| Modify | `internal/orchestrate/infra.go` | Add `AGENCY_CALLER` env var to each container |
| Create | `internal/orchestrate/infra_docker_audit.go` | Docker socket audit check at startup |
| Create | `internal/orchestrate/infra_docker_audit_test.go` | Tests for Docker socket audit |

---

## Phase 1: Decouple Inter-Service Communication

### Task 1: Add internal event publishing endpoint to gateway

The gateway needs a `POST /api/v1/events/publish` endpoint that infra services (intake, knowledge) can call to publish events to the event bus. This replaces direct service-to-service comms calls.

**Files:**
- Create: `internal/api/events/handlers_publish.go`
- Create: `internal/api/events/handlers_publish_test.go`
- Modify: `internal/api/events/routes.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/api/events/handlers_publish_test.go
package events

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/geoffbelknap/agency/internal/events"
	"github.com/geoffbelknap/agency/internal/models"
)

func TestPublishEvent(t *testing.T) {
	logger := slog.Default()
	bus := events.NewBus(logger, nil)

	var delivered []*models.Event
	bus.RegisterDelivery("agent", func(sub *events.Subscription, e *models.Event) error {
		delivered = append(delivered, e)
		return nil
	})

	h := &handler{deps: Deps{EventBus: bus, Logger: logger}}

	body := `{"source_type":"platform","source_name":"intake","event_type":"knowledge_update","data":{"summary":"new node"}}`
	req := httptest.NewRequest("POST", "/api/v1/events/publish", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.publishEvent(w, req)

	if w.Code != 202 {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != "published" {
		t.Fatalf("expected status=published, got %s", resp["status"])
	}
}

func TestPublishEvent_InvalidJSON(t *testing.T) {
	logger := slog.Default()
	bus := events.NewBus(logger, nil)
	h := &handler{deps: Deps{EventBus: bus, Logger: logger}}

	req := httptest.NewRequest("POST", "/api/v1/events/publish", bytes.NewBufferString("not json"))
	w := httptest.NewRecorder()
	h.publishEvent(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestPublishEvent_MissingFields(t *testing.T) {
	logger := slog.Default()
	bus := events.NewBus(logger, nil)
	h := &handler{deps: Deps{EventBus: bus, Logger: logger}}

	body := `{"source_type":"platform"}`
	req := httptest.NewRequest("POST", "/api/v1/events/publish", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.publishEvent(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestPublishEvent_NilBus(t *testing.T) {
	h := &handler{deps: Deps{Logger: slog.Default()}}

	body := `{"source_type":"platform","source_name":"intake","event_type":"test","data":{}}`
	req := httptest.NewRequest("POST", "/api/v1/events/publish", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.publishEvent(w, req)

	if w.Code != 503 {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/events/ -run TestPublishEvent -v`
Expected: FAIL — `publishEvent` method does not exist

- [ ] **Step 3: Write the publish handler**

```go
// internal/api/events/handlers_publish.go
package events

import (
	"encoding/json"
	"net/http"

	"github.com/geoffbelknap/agency/internal/models"
)

// publishEvent handles POST /api/v1/events/publish
// Internal endpoint for infra services to publish events to the event bus.
// Replaces direct service-to-service HTTP calls (e.g., intake→comms, knowledge→comms).
func (h *handler) publishEvent(w http.ResponseWriter, r *http.Request) {
	if h.deps.EventBus == nil {
		writeJSON(w, 503, map[string]string{"error": "event bus not initialized"})
		return
	}

	var body struct {
		SourceType string                 `json:"source_type"`
		SourceName string                 `json:"source_name"`
		EventType  string                 `json:"event_type"`
		Data       map[string]interface{} `json:"data"`
		Metadata   map[string]interface{} `json:"metadata,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	if body.SourceType == "" || body.SourceName == "" || body.EventType == "" {
		writeJSON(w, 400, map[string]string{"error": "source_type, source_name, and event_type are required"})
		return
	}

	event := models.NewEvent(body.SourceType, body.SourceName, body.EventType, body.Data)
	if body.Metadata != nil {
		event.Metadata = body.Metadata
	}
	h.deps.EventBus.Publish(event)

	writeJSON(w, 202, map[string]string{"status": "published", "event_id": event.ID})
}
```

- [ ] **Step 4: Register the route**

In `internal/api/events/routes.go`, add to the `RegisterRoutes` function:

```go
r.Post("/api/v1/events/publish", h.publishEvent)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/api/events/ -run TestPublishEvent -v`
Expected: All 4 tests PASS

- [ ] **Step 6: Commit**

```
feat: add internal event publishing endpoint for inter-service communication
```

### Task 2: Expose graph ingest and event publish on the socket router

The socket router (served via gateway-proxy at `gateway:8200`) doesn't include graph or event routes. Intake and knowledge need these endpoints to route through the gateway.

**Files:**
- Modify: `internal/api/routes.go:57-100`

- [ ] **Step 1: Write the failing test**

```go
// Add to internal/api/validation_test.go or create internal/api/socket_routes_test.go
func TestSocketRouterHasGraphIngest(t *testing.T) {
	r := chi.NewRouter()
	// RegisterSocketRoutes with minimal deps — we just check route registration
	// Use a walkFunc to check for the route
	found := false
	chi.Walk(r, func(method, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		if method == "POST" && route == "/api/v1/graph/ingest" {
			found = true
		}
		return nil
	})
	// This will fail until we add the route
	if !found {
		t.Fatal("POST /api/v1/graph/ingest not registered on socket router")
	}
}
```

Actually, the simpler approach: just add the routes and verify with an integration-style test. The existing `validation_test.go` pattern covers route registration.

- [ ] **Step 1: Add graph and event routes to socket router**

In `internal/api/routes.go`, inside `RegisterSocketRoutes`, after the `apicomms.RegisterRoutes` block, add:

```go
	// Graph ingest on the socket — used by intake to ingest connector data
	// into the knowledge graph via the gateway (hub-and-spoke).
	graph.RegisterRoutes(r, graph.Deps{
		Knowledge: startup.Knowledge,
		Logger:    logger,
	})

	// Internal event publishing — used by intake and knowledge to emit events
	// to the event bus instead of calling comms directly (hub-and-spoke).
	if opts.EventBus != nil {
		apievents.RegisterRoutes(r, apievents.Deps{
			EventBus:   opts.EventBus,
			WebhookMgr: opts.WebhookMgr,
			Scheduler:  opts.Scheduler,
			NotifStore: opts.NotifStore,
			Config:     cfg,
			Logger:     logger,
		})
	}
```

- [ ] **Step 2: Run existing tests to verify no regressions**

Run: `go test ./internal/api/... -v -count=1`
Expected: PASS

- [ ] **Step 3: Commit**

```
feat: expose graph ingest and event publish on gateway socket router
```

### Task 3: Create gateway client for intake service

Replace intake's direct HTTP calls to comms and knowledge with calls through the gateway.

**Files:**
- Create: `images/intake/gateway_client.py`
- Create: `images/tests/test_gateway_client.py`

- [ ] **Step 1: Write the failing test**

```python
# images/tests/test_gateway_client.py
import pytest
from unittest.mock import patch, MagicMock, AsyncMock
import json


class TestGatewayClient:
    """Tests for the intake gateway client."""

    def test_init_defaults(self):
        from intake.gateway_client import GatewayClient
        client = GatewayClient()
        assert client.base_url == "http://gateway:8200"

    def test_init_custom_url(self):
        from intake.gateway_client import GatewayClient
        client = GatewayClient(base_url="http://localhost:8200")
        assert client.base_url == "http://localhost:8200"

    @pytest.mark.asyncio
    async def test_publish_event(self):
        from intake.gateway_client import GatewayClient
        client = GatewayClient(base_url="http://test:8200")

        with patch("intake.gateway_client.aiohttp.ClientSession") as mock_session_cls:
            mock_session = AsyncMock()
            mock_resp = AsyncMock()
            mock_resp.status = 202
            mock_resp.json = AsyncMock(return_value={"status": "published", "event_id": "evt-123"})
            mock_session.post = AsyncMock(return_value=mock_resp)
            mock_session.__aenter__ = AsyncMock(return_value=mock_session)
            mock_session.__aexit__ = AsyncMock(return_value=False)
            mock_session_cls.return_value = mock_session

            await client.publish_event(
                source_name="intake",
                event_type="work_item_created",
                data={"item_id": "wi-001"},
            )

            mock_session.post.assert_called_once()
            call_args = mock_session.post.call_args
            assert "/api/v1/events/publish" in call_args[0][0]

    @pytest.mark.asyncio
    async def test_graph_ingest(self):
        from intake.gateway_client import GatewayClient
        client = GatewayClient(base_url="http://test:8200")

        with patch("intake.gateway_client.aiohttp.ClientSession") as mock_session_cls:
            mock_session = AsyncMock()
            mock_resp = AsyncMock()
            mock_resp.status = 200
            mock_resp.json = AsyncMock(return_value={"nodes_created": 3})
            mock_session.post = AsyncMock(return_value=mock_resp)
            mock_session.__aenter__ = AsyncMock(return_value=mock_session)
            mock_session.__aexit__ = AsyncMock(return_value=False)
            mock_session_cls.return_value = mock_session

            result = await client.graph_ingest(
                content='{"type": "alert", "id": "A-1"}',
                filename="alert.json",
                content_type="application/json",
            )

            mock_session.post.assert_called_once()
            call_args = mock_session.post.call_args
            assert "/api/v1/graph/ingest" in call_args[0][0]
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pytest images/tests/test_gateway_client.py -v`
Expected: FAIL — `intake.gateway_client` does not exist

- [ ] **Step 3: Write the gateway client**

```python
# images/intake/gateway_client.py
"""Gateway HTTP client for the intake service.

Routes inter-service calls through the gateway (hub-and-spoke)
instead of direct HTTP to comms/knowledge containers.
"""
import logging
import aiohttp

logger = logging.getLogger("agency.intake.gateway")


class GatewayClient:
    def __init__(self, base_url: str = "http://gateway:8200", token: str = ""):
        self.base_url = base_url.rstrip("/")
        self.token = token

    def _headers(self) -> dict:
        h = {"Content-Type": "application/json"}
        if self.token:
            h["Authorization"] = f"Bearer {self.token}"
        return h

    async def publish_event(
        self,
        source_name: str,
        event_type: str,
        data: dict,
        metadata: dict | None = None,
    ) -> None:
        """Publish an event to the gateway event bus."""
        payload = {
            "source_type": "platform",
            "source_name": source_name,
            "event_type": event_type,
            "data": data,
        }
        if metadata:
            payload["metadata"] = metadata

        url = f"{self.base_url}/api/v1/events/publish"
        try:
            async with aiohttp.ClientSession() as session:
                resp = await session.post(url, json=payload, headers=self._headers(), timeout=aiohttp.ClientTimeout(total=10))
                if resp.status >= 400:
                    body = await resp.text()
                    logger.warning("event publish failed: %d %s", resp.status, body)
        except Exception as e:
            logger.warning("event publish error: %s", e)

    async def graph_ingest(
        self,
        content: str,
        filename: str = "",
        content_type: str = "application/json",
    ) -> dict | None:
        """Ingest content into the knowledge graph via the gateway."""
        payload = {
            "content": content,
            "filename": filename,
            "content_type": content_type,
        }
        url = f"{self.base_url}/api/v1/graph/ingest"
        try:
            async with aiohttp.ClientSession() as session:
                resp = await session.post(url, json=payload, headers=self._headers(), timeout=aiohttp.ClientTimeout(total=30))
                if resp.status < 400:
                    return await resp.json()
                body = await resp.text()
                logger.warning("graph ingest failed: %d %s", resp.status, body)
        except Exception as e:
            logger.warning("graph ingest error: %s", e)
        return None
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `pytest images/tests/test_gateway_client.py -v`
Expected: PASS

- [ ] **Step 5: Commit**

```
feat: add gateway client for intake inter-service routing
```

### Task 4: Route intake calls through gateway

Replace all direct `comms_url` and `knowledge_url` HTTP calls in the intake server with gateway client calls.

**Files:**
- Modify: `images/intake/server.py`
- Modify: `internal/orchestrate/infra.go:806-810`

This is the largest Python change. The intake server has ~30 references to `comms_url` and `knowledge_url`. The key functions to change:

- `_deliver_to_comms()` — posts work items to comms channels → use `gateway_client.publish_event()`
- `_post_channel_message()` — posts messages to comms channels → use gateway event publish
- `_route_and_deliver()` — calls knowledge graph ingest → use `gateway_client.graph_ingest()`
- `_fetch_channel_messages()` — reads comms messages for channel watchers → use gateway comms endpoint
- `create_app()` — stores `comms_url` / `knowledge_url` → store `gateway_client` instead

- [ ] **Step 1: Add gateway_client to app initialization**

In `images/intake/server.py`, modify `create_app()`:

```python
def create_app(
    connectors_dir: Optional[Path] = None,
    data_dir: Optional[Path] = None,
    gateway_url: str = "http://gateway:8200",
    gateway_token: str = "",
) -> web.Application:
    from logging_config import correlation_middleware
    from gateway_client import GatewayClient

    app = web.Application(middlewares=[correlation_middleware()])
    app["connectors_dir"] = connectors_dir or Path("/app/connectors")
    app["connectors"] = _load_connectors(app["connectors_dir"])
    app["store"] = WorkItemStore(data_dir=data_dir or Path("/app/data"))
    app["gateway"] = GatewayClient(base_url=gateway_url, token=gateway_token)
    app["event_buffer"] = EventBuffer()
    # ... rest unchanged
```

- [ ] **Step 2: Replace `_deliver_to_comms` with gateway event publish**

Replace the direct comms POST with an event publish. Where the current code does:

```python
async with session.post(f"{comms_url}/tasks/deliver", json=payload) as resp:
```

Replace with:

```python
await gateway.publish_event(
    source_name=f"intake:{connector_name}",
    event_type="work_item_created",
    data={"channel": channel, "content": content, "author": author},
)
```

- [ ] **Step 3: Replace `_post_channel_message` with gateway comms route**

The gateway socket router already registers comms routes. Change:

```python
async with session.post(f"{comms_url}/channels/{channel_name}/messages", json=body) as resp:
```

To:

```python
async with session.post(f"{gateway_url}/api/v1/comms/channels/{channel_name}/messages", json=body, headers=headers) as resp:
```

- [ ] **Step 4: Replace knowledge graph ingest with gateway route**

In `_route_and_deliver`, where intake calls `evaluate_graph_ingest()` with `knowledge_url`, change to use `gateway.graph_ingest()`.

- [ ] **Step 5: Replace `_fetch_channel_messages` with gateway comms route**

Change:

```python
async with session.get(f"{comms_url}/channels/{channel}/messages", params=params) as resp:
```

To:

```python
async with session.get(f"{gateway_url}/api/v1/comms/channels/{channel}/messages", params=params, headers=headers) as resp:
```

- [ ] **Step 6: Update CLI args and env vars**

In `main()`, replace `--comms-url` and `--knowledge-url` args with `--gateway-url`:

```python
parser.add_argument("--gateway-url", type=str, default=os.environ.get("GATEWAY_URL", "http://gateway:8200"))
parser.add_argument("--gateway-token", type=str, default=os.environ.get("GATEWAY_TOKEN", ""))
```

- [ ] **Step 7: Update infra.go intake container env vars**

In `internal/orchestrate/infra.go`, in `ensureIntake()`, remove `KNOWLEDGE_URL` env var and `comms,knowledge` from `NO_PROXY`:

```go
env := map[string]string{
    "HTTP_PROXY":    "http://egress:3128",
    "HTTPS_PROXY":   "http://egress:3128",
    "NO_PROXY":      "gateway,localhost,127.0.0.1",
    "GATEWAY_URL":   "http://gateway:8200",
    "GATEWAY_TOKEN": inf.GatewayToken,
}
```

- [ ] **Step 8: Run intake tests**

Run: `pytest images/tests/ -k intake -v`
Expected: PASS (some tests may need updating for removed `comms_url` parameter)

- [ ] **Step 9: Commit**

```
refactor: route intake inter-service calls through gateway
```

### Task 5: Route knowledge comms calls through gateway

Replace knowledge's direct comms HTTP calls with gateway event bus publishes.

**Files:**
- Create: `images/knowledge/gateway_client.py`
- Modify: `images/knowledge/server.py:68-78,169,635`
- Modify: `internal/orchestrate/infra.go:722-724`

- [ ] **Step 1: Create knowledge gateway client**

```python
# images/knowledge/gateway_client.py
"""Gateway client for the knowledge service.

Routes curator notifications through the gateway event bus
instead of direct HTTP to the comms container.
"""
import logging
import httpx

logger = logging.getLogger("agency.knowledge.gateway")


class GatewayClient:
    def __init__(self, base_url: str = "http://gateway:8200", token: str = ""):
        self.base_url = base_url.rstrip("/")
        self.token = token

    def _headers(self) -> dict:
        h = {"Content-Type": "application/json"}
        if self.token:
            h["Authorization"] = f"Bearer {self.token}"
        return h

    def publish_knowledge_update(self, node_summary: str, metadata: dict) -> None:
        """Publish a knowledge update event via the gateway event bus."""
        payload = {
            "source_type": "platform",
            "source_name": "knowledge",
            "event_type": "knowledge_update",
            "data": {
                "summary": node_summary,
                "channel": "_knowledge-updates",
                **metadata,
            },
        }
        try:
            client = httpx.Client(timeout=5)
            resp = client.post(
                f"{self.base_url}/api/v1/events/publish",
                json=payload,
                headers=self._headers(),
            )
            if resp.status_code >= 400:
                logger.warning("knowledge update publish failed: %d", resp.status_code)
        except Exception as e:
            logger.warning("knowledge update publish error: %s", e)
```

- [ ] **Step 2: Replace `publish_knowledge_update` function**

In `images/knowledge/server.py`, replace the standalone `publish_knowledge_update()` function (lines 68-78) that calls comms directly:

```python
def publish_knowledge_update(gateway: "GatewayClient", node_summary: str, metadata: dict) -> None:
    gateway.publish_knowledge_update(node_summary, metadata)
```

Update all callers to pass the gateway client from `app["gateway"]`.

- [ ] **Step 3: Update knowledge app initialization**

In `images/knowledge/server.py`, where `comms_url` is stored in the app (line 169,174):

```python
from gateway_client import GatewayClient
gateway_url = os.environ.get("AGENCY_GATEWAY_URL", "http://gateway:8200")
gateway_token = os.environ.get("AGENCY_GATEWAY_TOKEN", "")
app["gateway"] = GatewayClient(base_url=gateway_url, token=gateway_token)
```

Remove `app["comms_url"]`.

- [ ] **Step 4: Update knowledge infra.go env vars**

In `internal/orchestrate/infra.go`, in `ensureKnowledge()`, update the `NO_PROXY`:

```go
env := map[string]string{
    "HTTPS_PROXY":          "http://egress:3128",
    "NO_PROXY":             "agency-infra-embeddings,localhost,127.0.0.1,gateway",
    "AGENCY_GATEWAY_TOKEN": inf.GatewayToken,
    "AGENCY_GATEWAY_URL":   "http://gateway:8200",
}
```

The `NO_PROXY` already includes `gateway` — just remove any comms/knowledge cross-service hostnames if present.

- [ ] **Step 5: Run knowledge tests**

Run: `pytest images/tests/ -k knowledge -v`
Expected: PASS

- [ ] **Step 6: Commit**

```
refactor: route knowledge comms calls through gateway event bus
```

### Task 6: Update web-fetch NO_PROXY

**Files:**
- Modify: `internal/orchestrate/infra.go:881-883`

- [ ] **Step 1: Update NO_PROXY**

In `ensureWebFetch()`, change:

```go
"NO_PROXY": "comms,knowledge,localhost,127.0.0.1",
```

To:

```go
"NO_PROXY": "gateway,localhost,127.0.0.1",
```

Web-fetch doesn't call comms or knowledge — these were copy-paste from intake.

- [ ] **Step 2: Run Go tests**

Run: `go test ./internal/orchestrate/... -v -count=1`
Expected: PASS

- [ ] **Step 3: Commit**

```
fix: remove stale comms/knowledge from web-fetch NO_PROXY
```

---

## Phase 2: Network Topology Swap

### Task 7: Add CreateMediationNetwork factory

**Files:**
- Modify: `internal/orchestrate/containers/networks.go`
- Modify: `internal/orchestrate/containers/networks_test.go`

- [ ] **Step 1: Write the failing test**

```go
// Add to internal/orchestrate/containers/networks_test.go
func TestCreateMediationNetwork(t *testing.T) {
	mock := &mockNetworkAPI{created: make(map[string]network.CreateOptions)}
	err := CreateMediationNetwork(context.Background(), mock, "agency-gateway", nil)
	if err != nil {
		t.Fatal(err)
	}
	opts, ok := mock.created["agency-gateway"]
	if !ok {
		t.Fatal("network not created")
	}
	if opts.Driver != "bridge" {
		t.Fatalf("expected bridge, got %s", opts.Driver)
	}
	if !opts.Internal {
		t.Fatal("mediation network must be internal")
	}
	if opts.Labels["agency.managed"] != "true" {
		t.Fatal("missing managed label")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/orchestrate/containers/ -run TestCreateMediationNetwork -v`
Expected: FAIL — `CreateMediationNetwork` does not exist

- [ ] **Step 3: Write the factory**

```go
// Add to internal/orchestrate/containers/networks.go

// CreateMediationNetwork creates an internal bridge network for service mediation.
// Used for agency-gateway and agency-egress-int — internal networks with no
// external route, enforcing the mediation boundary (ASK tenet 3).
func CreateMediationNetwork(ctx context.Context, cli NetworkAPI, name string, labels map[string]string) error {
	_, err := cli.NetworkCreate(ctx, name, network.CreateOptions{
		Driver:   "bridge",
		Internal: true,
		Labels:   mergeLabels(labels),
	})
	return err
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/orchestrate/containers/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```
feat: add CreateMediationNetwork factory for hub-and-spoke topology
```

### Task 8: Swap network constants and ensureNetworks

**Files:**
- Modify: `internal/orchestrate/infra.go:33-39,463-499`

- [ ] **Step 1: Update network constants**

```go
const (
	prefix       = "agency"
	gatewayNet   = "agency-gateway"
	egressIntNet = "agency-egress-int"
	egressExtNet = "agency-egress-ext"
	operatorNet  = "agency-operator"
)
```

Remove `mediationNet`, `egressNet`, and `internalNet` constants.

- [ ] **Step 2: Update ensureNetworks**

```go
func (inf *Infra) ensureNetworks(ctx context.Context) error {
	type netSpec struct {
		name     string
		internal bool
	}
	nets := []netSpec{
		{gatewayNet, true},    // Hub — all services and enforcers
		{egressIntNet, true},  // Services → egress proxy
		{egressExtNet, false}, // Egress proxy → internet
		{operatorNet, false},  // Operator tools (web, relay)
	}
	for _, n := range nets {
		_, inspectErr := inf.cli.NetworkInspect(ctx, n.name, network.InspectOptions{})
		if inspectErr != nil {
			var err error
			switch {
			case n.internal:
				err = containers.CreateMediationNetwork(ctx, inf.cli, n.name, nil)
			case n.name == operatorNet:
				err = containers.CreateOperatorNetwork(ctx, inf.cli, n.name, nil)
			default:
				err = containers.CreateEgressNetwork(ctx, inf.cli, n.name, nil)
			}
			if err != nil {
				return fmt.Errorf("create network %s: %w", n.name, err)
			}
			inf.log.Debug("created network", "name", n.name, "internal", n.internal)
		}
	}
	return nil
}
```

- [ ] **Step 3: Find and update all references to old constants**

Search for `mediationNet`, `egressNet`, `internalNet` throughout the file and update:
- `mediationNet` → `gatewayNet` (in most places) or `egressIntNet` (for egress connections)
- `egressNet` → `egressExtNet`
- `internalNet` — delete (was unused)

- [ ] **Step 4: Run Go tests**

Run: `go test ./internal/orchestrate/... -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```
refactor: replace flat mediation network with hub-and-spoke topology constants
```

### Task 9: Update gateway-proxy, egress, and infra container network assignments

**Files:**
- Modify: `internal/orchestrate/infra.go` (ensureGatewayProxy, ensureEgress, ensureComms, ensureKnowledge, ensureIntake, ensureWebFetch, ensureEmbeddings)

- [ ] **Step 1: Update ensureGatewayProxy**

Change `NetworkMode` from `mediationNet` to `gatewayNet`. Update network alias. Remove `ExtraHosts`.

```go
hc.NetworkMode = container.NetworkMode(gatewayNet)
// Remove: hc.ExtraHosts = []string{"gateway:host-gateway"}

netCfg := &network.NetworkingConfig{
    EndpointsConfig: map[string]*network.EndpointSettings{
        gatewayNet: {
            Aliases: []string{"gateway"},
        },
    },
}
```

- [ ] **Step 2: Update ensureEgress**

Primary network: `egressIntNet`. Connect to `gatewayNet` and `egressExtNet`. Remove `ExtraHosts`.

```go
hc.NetworkMode = container.NetworkMode(egressIntNet)
// Remove: hc.ExtraHosts = []string{"gateway:host-gateway"}

// After CreateAndStart:
inf.connectIfNeeded(ctx, id, gatewayNet, []string{"egress"})
inf.connectIfNeeded(ctx, id, egressExtNet, []string{"egress"})
```

- [ ] **Step 3: Update ensureComms**

Change `NetworkMode` from `mediationNet` to `gatewayNet`.

```go
hc.NetworkMode = container.NetworkMode(gatewayNet)
```

- [ ] **Step 4: Update ensureKnowledge**

Primary network: `gatewayNet`. Connect to `egressIntNet`. Remove `connectToAgentNetworks` call.

```go
hc.NetworkMode = container.NetworkMode(gatewayNet)

// After CreateAndStart and waitHealthy:
inf.connectIfNeeded(ctx, id, egressIntNet, []string{"knowledge"})
// Remove: inf.connectToAgentNetworks(ctx, name, "knowledge")
```

Note: `ensureKnowledge` currently doesn't capture the container ID from `CreateAndStart`. Update to capture it:

```go
id, err := containers.CreateAndStart(ctx, inf.cli, name, ...)
```

- [ ] **Step 5: Update ensureIntake**

Primary network: `gatewayNet`. Connect to `egressIntNet`.

```go
hc.NetworkMode = container.NetworkMode(gatewayNet)

// After CreateAndStart:
inf.connectIfNeeded(ctx, id, egressIntNet, []string{"intake"})
```

Same note: capture container ID from `CreateAndStart`.

- [ ] **Step 6: Update ensureWebFetch**

Primary network: `gatewayNet`. Connect to `egressIntNet`.

```go
hc.NetworkMode = container.NetworkMode(gatewayNet)

// After CreateAndStart:
inf.connectIfNeeded(ctx, id, egressIntNet, []string{"web-fetch"})
```

Same note: capture container ID.

- [ ] **Step 7: Update ensureEmbeddings**

Change `NetworkMode` from `mediationNet` to `gatewayNet`.

```go
hc.NetworkMode = container.NetworkMode(gatewayNet)
```

- [ ] **Step 8: Delete connectToAgentNetworks**

Remove the `connectToAgentNetworks` function (lines 1320-1341). It was only called from `ensureKnowledge`.

- [ ] **Step 9: Run Go tests**

Run: `go test ./internal/orchestrate/... -v -count=1`
Expected: PASS

- [ ] **Step 10: Commit**

```
refactor: migrate all infra containers to hub-and-spoke network topology
```

### Task 10: Update enforcer network connections

**Files:**
- Modify: `internal/orchestrate/enforcer.go:225-228`

- [ ] **Step 1: Update enforcer mediation network connection**

In `enforcer.go`, the enforcer currently connects to `mediationNet` after starting. Change to connect to both `gatewayNet` and `egressIntNet`:

```go
// Connect to gateway network (hub — service access, signals, budget)
_ = e.cli.NetworkConnect(ctx, gatewayNet, containerID, &network.EndpointSettings{
    Aliases: []string{"enforcer"},
})

// Connect to egress network (LLM proxy)
_ = e.cli.NetworkConnect(ctx, egressIntNet, containerID, &network.EndpointSettings{
    Aliases: []string{"enforcer"},
})
```

- [ ] **Step 2: Run Go tests**

Run: `go test ./internal/orchestrate/... -v -count=1`
Expected: PASS

- [ ] **Step 3: Commit**

```
refactor: connect enforcer to gateway + egress-int networks
```

---

## Phase 3: Socket Hardening

### Task 11: Add X-Agency-Caller middleware

**Files:**
- Create: `internal/api/middleware_caller.go`
- Create: `internal/api/middleware_caller_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/api/middleware_caller_test.go
package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCallerMiddleware_AllowedCaller(t *testing.T) {
	allowlist := map[string][]string{
		"POST /api/v1/agents/{name}/signal": {"enforcer"},
	}
	mw := CallerValidation(allowlist)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("POST", "/api/v1/agents/alice/signal", nil)
	req.Header.Set("X-Agency-Caller", "enforcer")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestCallerMiddleware_BlockedCaller(t *testing.T) {
	allowlist := map[string][]string{
		"POST /api/v1/agents/{name}/signal": {"enforcer"},
	}
	mw := CallerValidation(allowlist)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("POST", "/api/v1/agents/alice/signal", nil)
	req.Header.Set("X-Agency-Caller", "intake")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 403 {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestCallerMiddleware_NoAllowlist(t *testing.T) {
	allowlist := map[string][]string{}
	mw := CallerValidation(allowlist)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/api/v1/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200 for unprotected route, got %d", w.Code)
	}
}

func TestCallerMiddleware_MissingHeader(t *testing.T) {
	allowlist := map[string][]string{
		"POST /api/v1/agents/{name}/signal": {"enforcer"},
	}
	mw := CallerValidation(allowlist)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("POST", "/api/v1/agents/alice/signal", nil)
	// No X-Agency-Caller header
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 403 {
		t.Fatalf("expected 403 for missing caller header, got %d", w.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestCallerMiddleware -v`
Expected: FAIL — `CallerValidation` does not exist

- [ ] **Step 3: Write the middleware**

```go
// internal/api/middleware_caller.go
package api

import (
	"net/http"
	"strings"
)

// CallerValidation returns middleware that validates X-Agency-Caller headers
// against a per-route allowlist. Routes not in the allowlist are unrestricted.
// This is defense-in-depth, not authentication — a compromised container can
// spoof the header.
func CallerValidation(allowlist map[string][]string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Find matching allowlist entry
			allowed := findAllowed(allowlist, r.Method, r.URL.Path)
			if allowed == nil {
				// No allowlist entry — route is unrestricted
				next.ServeHTTP(w, r)
				return
			}

			caller := r.Header.Get("X-Agency-Caller")
			if caller == "" {
				writeJSON(w, 403, map[string]string{"error": "X-Agency-Caller header required"})
				return
			}

			for _, a := range allowed {
				if a == caller || a == "any" {
					next.ServeHTTP(w, r)
					return
				}
			}

			writeJSON(w, 403, map[string]string{"error": "caller not authorized for this endpoint"})
		})
	}
}

// findAllowed matches a request against the allowlist, supporting {param} wildcards.
func findAllowed(allowlist map[string][]string, method, path string) []string {
	for pattern, callers := range allowlist {
		parts := strings.SplitN(pattern, " ", 2)
		if len(parts) != 2 {
			continue
		}
		if parts[0] != method {
			continue
		}
		if matchPath(parts[1], path) {
			return callers
		}
	}
	return nil
}

// matchPath matches a URL path against a pattern with {param} and * wildcards.
func matchPath(pattern, path string) bool {
	patParts := strings.Split(strings.Trim(pattern, "/"), "/")
	pathParts := strings.Split(strings.Trim(path, "/"), "/")

	if strings.HasSuffix(pattern, "/*") {
		// Prefix match for wildcard patterns
		prefix := patParts[:len(patParts)-1]
		if len(pathParts) < len(prefix) {
			return false
		}
		for i, p := range prefix {
			if strings.HasPrefix(p, "{") {
				continue
			}
			if p != pathParts[i] {
				return false
			}
		}
		return true
	}

	if len(patParts) != len(pathParts) {
		return false
	}
	for i, p := range patParts {
		if strings.HasPrefix(p, "{") {
			continue
		}
		if p != pathParts[i] {
			return false
		}
	}
	return true
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/api/ -run TestCallerMiddleware -v`
Expected: PASS

- [ ] **Step 5: Commit**

```
feat: add X-Agency-Caller validation middleware for socket routes
```

### Task 12: Apply caller middleware to socket routes and add AGENCY_CALLER env

**Files:**
- Modify: `internal/api/routes.go:57-100`
- Modify: `internal/orchestrate/infra.go` (each ensure* function)

- [ ] **Step 1: Apply middleware to socket router**

In `RegisterSocketRoutes`, add the caller validation middleware:

```go
func RegisterSocketRoutes(r chi.Router, cfg *config.Config, dc *docker.Client, logger *slog.Logger, startup *StartupResult, opts RouteOptions) {
	// Defense-in-depth: validate X-Agency-Caller on protected endpoints
	callerAllowlist := map[string][]string{
		"POST /api/v1/agents/{name}/signal":    {"enforcer"},
		"POST /api/v1/infra/internal/llm":      {"enforcer", "knowledge"},
		"POST /api/v1/comms/channels/*":         {"comms", "intake"},
		"GET /api/v1/comms/channels/*":          {"comms", "intake", "enforcer"},
		"POST /api/v1/graph/ingest":             {"intake"},
		"POST /api/v1/events/publish":           {"intake", "knowledge"},
		"GET /api/v1/creds/internal/resolve":    {"egress"},
	}
	r.Use(CallerValidation(callerAllowlist))

	// ... existing route registrations
```

- [ ] **Step 2: Add AGENCY_CALLER env var to each container**

In each `ensure*` function in `infra.go`, add the env var:

```go
// In ensureEgress:
env["AGENCY_CALLER"] = "egress"

// In ensureComms:
env["AGENCY_CALLER"] = "comms"

// In ensureKnowledge:
env["AGENCY_CALLER"] = "knowledge"

// In ensureIntake:
env["AGENCY_CALLER"] = "intake"

// In ensureWebFetch:
env["AGENCY_CALLER"] = "web-fetch"
```

In `enforcer.go`:
```go
env["AGENCY_CALLER"] = "enforcer"
```

- [ ] **Step 3: Update Python services to send the header**

In `images/intake/gateway_client.py` and `images/knowledge/gateway_client.py`, add the caller header:

```python
def _headers(self) -> dict:
    h = {"Content-Type": "application/json"}
    if self.token:
        h["Authorization"] = f"Bearer {self.token}"
    caller = os.environ.get("AGENCY_CALLER", "")
    if caller:
        h["X-Agency-Caller"] = caller
    return h
```

- [ ] **Step 4: Run all tests**

Run: `go test ./... -count=1` and `pytest images/tests/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```
feat: apply caller validation middleware to socket routes
```

### Task 13: Add Docker socket audit check

**Files:**
- Create: `internal/orchestrate/infra_docker_audit.go`
- Create: `internal/orchestrate/infra_docker_audit_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/orchestrate/infra_docker_audit_test.go
package orchestrate

import (
	"context"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
)

func TestAuditDockerSocket_Clean(t *testing.T) {
	containers := []types.Container{
		{
			Names:  []string{"/agency-infra-egress"},
			Labels: map[string]string{"agency.managed": "true"},
			Mounts: []types.MountPoint{
				{Source: "/home/user/.agency/run", Destination: "/run"},
			},
		},
	}
	violations := checkDockerSocketMounts(containers)
	if len(violations) != 0 {
		t.Fatalf("expected 0 violations, got %d", len(violations))
	}
}

func TestAuditDockerSocket_Violation(t *testing.T) {
	containers := []types.Container{
		{
			Names:  []string{"/agency-infra-evil"},
			Labels: map[string]string{"agency.managed": "true"},
			Mounts: []types.MountPoint{
				{Source: "/var/run/docker.sock", Destination: "/var/run/docker.sock"},
			},
		},
	}
	violations := checkDockerSocketMounts(containers)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0] != "/agency-infra-evil" {
		t.Fatalf("expected evil container, got %s", violations[0])
	}
}

func TestAuditDockerSocket_SkipsUnmanaged(t *testing.T) {
	containers := []types.Container{
		{
			Names:  []string{"/some-other-container"},
			Labels: map[string]string{},
			Mounts: []types.MountPoint{
				{Source: "/var/run/docker.sock", Destination: "/var/run/docker.sock"},
			},
		},
	}
	violations := checkDockerSocketMounts(containers)
	if len(violations) != 0 {
		t.Fatalf("expected 0 violations for unmanaged container, got %d", len(violations))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/orchestrate/ -run TestAuditDockerSocket -v`
Expected: FAIL — `checkDockerSocketMounts` does not exist

- [ ] **Step 3: Write the audit function**

```go
// internal/orchestrate/infra_docker_audit.go
package orchestrate

import (
	"context"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
)

// AuditDockerSocket checks all agency.managed containers for Docker socket mounts.
// Returns container names that have /var/run/docker.sock mounted — a security violation.
func (inf *Infra) AuditDockerSocket(ctx context.Context) []string {
	containers, err := inf.cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", "agency.managed=true")),
	})
	if err != nil {
		inf.log.Warn("docker socket audit: failed to list containers", "error", err)
		return nil
	}

	violations := checkDockerSocketMounts(containers)
	for _, name := range violations {
		inf.log.Error("SECURITY: container has Docker socket mounted", "container", name)
	}
	return violations
}

// checkDockerSocketMounts inspects container mount points for Docker socket access.
func checkDockerSocketMounts(containers []types.Container) []string {
	var violations []string
	for _, c := range containers {
		if c.Labels["agency.managed"] != "true" {
			continue
		}
		for _, m := range c.Mounts {
			if strings.Contains(m.Source, "docker.sock") {
				name := ""
				if len(c.Names) > 0 {
					name = c.Names[0]
				}
				violations = append(violations, name)
				break
			}
		}
	}
	return violations
}
```

- [ ] **Step 4: Call audit from Infra.Up**

In `internal/orchestrate/infra.go`, in the `Up` method, after all components are started, add:

```go
// Audit: verify no managed container has Docker socket access
if violations := inf.AuditDockerSocket(ctx); len(violations) > 0 {
    inf.log.Error("Docker socket audit FAILED — containers with /var/run/docker.sock mounted",
        "containers", violations)
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/orchestrate/ -run TestAuditDockerSocket -v`
Expected: PASS

- [ ] **Step 6: Commit**

```
feat: add Docker socket audit check at gateway startup
```

---

## Verification

### Task 14: End-to-end verification

- [ ] **Step 1: Build all images**

Run: `make images`
Expected: All images build successfully

- [ ] **Step 2: Run Go test suite**

Run: `go test ./... -count=1`
Expected: PASS

- [ ] **Step 3: Run Python test suite**

Run: `pytest images/tests/ -v`
Expected: PASS

- [ ] **Step 4: Start infrastructure and verify network topology**

Run: `agency infra up`

Then verify networks:
```bash
docker network ls | grep agency
```

Expected networks:
- `agency-gateway`
- `agency-egress-int`
- `agency-egress-ext`
- `agency-operator`

Old networks should NOT exist:
- `agency-mediation`
- `agency-internal`
- `agency-egress-net`

- [ ] **Step 5: Verify container network assignments**

```bash
docker inspect agency-infra-gateway-proxy --format '{{json .NetworkSettings.Networks}}' | python3 -m json.tool
docker inspect agency-infra-egress --format '{{json .NetworkSettings.Networks}}' | python3 -m json.tool
docker inspect agency-infra-comms --format '{{json .NetworkSettings.Networks}}' | python3 -m json.tool
docker inspect agency-infra-knowledge --format '{{json .NetworkSettings.Networks}}' | python3 -m json.tool
```

Expected:
- gateway-proxy: `agency-gateway` only
- egress: `agency-egress-int` + `agency-gateway` + `agency-egress-ext`
- comms: `agency-gateway` only
- knowledge: `agency-gateway` + `agency-egress-int`

- [ ] **Step 6: Commit final verification notes**

```
docs: mediation network hardening — implementation complete
```
