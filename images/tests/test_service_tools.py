import json

import httpx

from images.body.tools import ServiceToolDispatcher


def test_service_tool_dispatcher_supports_passthrough_schema(tmp_path):
    manifest = {
        "services": [{
            "service": "runtime_drive_admin",
            "api_base": "http://enforcer:8081/mediation/runtime",
            "scoped_token": "runtime",
            "tools": [{
                "name": "instance_community_admin_drive_admin_list_permissions",
                "description": "Projected runtime tool",
                "method": "POST",
                "path": "/api/v1/instances/inst_drive/runtime/nodes/drive_admin/actions/list_permissions",
                "parameters": [],
                "passthrough": True,
            }],
        }],
    }
    path = tmp_path / "services-manifest.json"
    path.write_text(json.dumps(manifest))

    dispatcher = ServiceToolDispatcher(path)
    dispatcher.load()
    defs = dispatcher.get_tool_definitions()

    assert defs[0]["function"]["parameters"]["additionalProperties"] is True
    assert defs[0]["function"]["parameters"]["properties"] == {}


def test_service_tool_dispatcher_posts_passthrough_arguments(tmp_path):
    manifest = {
        "services": [{
            "service": "runtime_drive_admin",
            "api_base": "http://enforcer:8081/mediation/runtime",
            "scoped_token": "runtime",
            "tools": [{
                "name": "instance_community_admin_drive_admin_list_permissions",
                "description": "Projected runtime tool",
                "method": "POST",
                "path": "/api/v1/instances/inst_drive/runtime/nodes/drive_admin/actions/list_permissions",
                "parameters": [],
                "passthrough": True,
            }],
        }],
    }
    path = tmp_path / "services-manifest.json"
    path.write_text(json.dumps(manifest))

    seen = {}

    def handler(request):
        seen["url"] = str(request.url)
        seen["tool"] = request.headers.get("X-Agency-Tool")
        seen["body"] = json.loads(request.content.decode())
        return httpx.Response(200, json={"ok": True})

    dispatcher = ServiceToolDispatcher(path)
    dispatcher.load()
    client = httpx.Client(transport=httpx.MockTransport(handler))

    result = dispatcher.call_tool(
        "instance_community_admin_drive_admin_list_permissions",
        {"resource_id": "file-123", "consent_provided": False},
        client,
    )

    assert json.loads(result) == {"ok": True}
    assert seen["url"] == "http://enforcer:8081/mediation/runtime/api/v1/instances/inst_drive/runtime/nodes/drive_admin/actions/list_permissions"
    assert seen["tool"] == "instance_community_admin_drive_admin_list_permissions"
    assert seen["body"] == {"resource_id": "file-123", "consent_provided": False}
