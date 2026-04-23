from pact_engine import ActivationContext, classify_activation


HANK_REPLAY_PROMPT = (
    "I want to see if you can help me out by investigating this github "
    "repository https://github.com/safishamsi/graphify and looking "
    "especially at the number of stars it has. I suspect many of these "
    "are \"paid\" stars, from fake accounts. Looking at the people who "
    "starred this repository, is there any pattern that that emerges "
    "that indicates inauthentic activity?"
)


def _contract_for(content: str):
    return classify_activation(ActivationContext.from_message(content))


def test_analytical_verbs_route_to_current_info():
    cases = [
        "I want you to investigate this github repository",
        "Can you investigate the graphify repo?",
        "investigate https://github.com/x/y",
        "Please analyze the release patterns",
        "Can you analyze these numbers for me?",
        "Give me an analysis of the recent commits",
        "Research the history of X",
        "Can you examine this file?",
        "Inspect the repository",
        "Assess whether this project is healthy",
        "Audit the stargazers",
        "Can you check the repo?",
        "Verify the number of stars",
        "Tell me about safishamsi/graphify",
        "Look at this commit: https://github.com/x/y/commit/abc",
        "Take a look at this",
        "Help me understand what's happening in this repo",
        "What's the deal with this project?",
        "What is this repository about?",
        "Who is the author of this code?",
    ]

    for content in cases:
        assert _contract_for(content).kind == "current_info"


def test_false_positive_regression_guards_do_not_route_to_current_info():
    cases = [
        "What is recursion?",
        "think about this",
        "Consider the options",
        "Can you help me understand TCP handshakes?",
    ]

    for content in cases:
        assert _contract_for(content).kind == "chat"


def test_past_tense_analyzed_matches_explicit_analytical_trigger():
    assert _contract_for("I analyzed my calendar this morning").kind == "current_info"


def test_investigate_alone_matches_current_info():
    assert _contract_for("investigate").kind == "current_info"


def test_hank_replay_integration_routes_to_current_info_with_evidence_contract():
    contract = _contract_for(HANK_REPLAY_PROMPT)

    assert contract.kind == "current_info"
    assert contract.requires_action is True
    assert "current_source_or_blocker" in contract.required_evidence


def test_operator_blocked_still_wins_over_current_info():
    assert _contract_for("I am blocked and need you to investigate this repo").kind == "operator_blocked"


def test_current_info_still_wins_over_lower_priority_contracts():
    cases = [
        "Analyze and fix the code bug",
        "Analyze and create a report file",
        "Analyze the repository",
    ]

    for content in cases:
        assert _contract_for(content).kind == "current_info"
