DESCRIPTION = "An agent only does what it has the rights for."

def pre_tool(call, verdict):
    if call.origin.kind == "agent" and call.tool.right and call.tool.right not in call.origin.rights:
        return deny(text("no_right", call.tool.right))
