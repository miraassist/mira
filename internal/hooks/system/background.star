DESCRIPTION = "In the background only an agent with rights changes something."

def pre_tool(call, verdict):
    if call.where.channel == "background" and not call.tool.read_only and call.origin.kind != "agent":
        return deny(text("deny_background"))
