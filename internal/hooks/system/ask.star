DESCRIPTION = "Ask only when someone can answer."

def pre_tool(call, verdict):
    if verdict.kind == "confirm" and not call.origin.can_ask:
        return deny(text("deny_no_confirm"))
