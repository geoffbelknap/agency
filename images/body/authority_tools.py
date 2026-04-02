"""Authority tools for function agents.

Provides halt_agent and recommend_exception tools that emit signals
for the platform to act on. Only function agents with appropriate
configuration will have these signals honored.
"""

import json


def register_authority_tools(registry, signal_fn, agent_name: str) -> None:
    """Register authority tools for function agents.

    Args:
        registry: Tool registry to register with.
        signal_fn: Callable to emit signals (body._emit_signal).
        agent_name: This agent's name.
    """
    registry.register_tool(
        name="halt_agent",
        description=(
            "Request that another agent in your team be halted. Only works "
            "if you are a function agent with halt_authority configured in "
            "your team. The platform validates your authority before acting."
        ),
        parameters={
            "type": "object",
            "properties": {
                "target": {
                    "type": "string",
                    "description": "Name of the agent to halt",
                },
                "halt_type": {
                    "type": "string",
                    "enum": ["supervised", "immediate"],
                    "description": "Halt type (default: supervised)",
                },
                "reason": {
                    "type": "string",
                    "description": "Reason for halting the agent",
                },
            },
            "required": ["target", "reason"],
        },
        handler=lambda args: _halt_agent(signal_fn, agent_name, args),
    )

    registry.register_tool(
        name="recommend_exception",
        description=(
            "Submit an advisory recommendation on a pending exception "
            "request. Only function agents can recommend. Recommendations "
            "are informational -- human approvers see them when reviewing."
        ),
        parameters={
            "type": "object",
            "properties": {
                "request_id": {
                    "type": "string",
                    "description": "Exception request ID",
                },
                "action": {
                    "type": "string",
                    "enum": ["approve", "deny"],
                    "description": "Recommended action",
                },
                "reasoning": {
                    "type": "string",
                    "description": "Explanation for the recommendation",
                },
            },
            "required": ["request_id", "action", "reasoning"],
        },
        handler=lambda args: _recommend_exception(signal_fn, agent_name, args),
    )


def _halt_agent(signal_fn, agent_name: str, args: dict) -> str:
    target = args.get("target", "")
    halt_type = args.get("halt_type", "supervised")
    reason = args.get("reason", "")

    if not target:
        return json.dumps({"error": "target is required"})
    if not reason:
        return json.dumps({"error": "reason is required"})

    signal_fn("halt_request", {
        "initiator": agent_name,
        "target": target,
        "halt_type": halt_type,
        "reason": reason,
    })

    return json.dumps({
        "status": "halt_request_submitted",
        "target": target,
        "halt_type": halt_type,
        "message": f"Halt request for {target} submitted. The platform will validate your authority and act accordingly.",
    })


def _recommend_exception(signal_fn, agent_name: str, args: dict) -> str:
    request_id = args.get("request_id", "")
    action = args.get("action", "")
    reasoning = args.get("reasoning", "")

    if not request_id:
        return json.dumps({"error": "request_id is required"})
    if action not in ("approve", "deny"):
        return json.dumps({"error": "action must be 'approve' or 'deny'"})
    if not reasoning:
        return json.dumps({"error": "reasoning is required"})

    signal_fn("exception_recommendation", {
        "agent": agent_name,
        "request_id": request_id,
        "action": action,
        "reasoning": reasoning,
    })

    return json.dumps({
        "status": "recommendation_submitted",
        "request_id": request_id,
        "action": action,
        "message": f"Recommendation ({action}) submitted for {request_id}. Human approvers will see this when reviewing.",
    })
