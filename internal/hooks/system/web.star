DESCRIPTION = "After web content nothing critical; keeping or passing something on only with a yes."

def pre_tool(call, verdict):
    # A web page could have steered the call: what needs a yes is locked,
    # even when the user says yes.
    if verdict.kind == "confirm" and call.turn.web_read:
        return deny(text("deny_after_web"))
    # What a page could talk the model into keeping or passing on asks first.
    if call.tool.keeps_or_passes_on and call.turn.foreign:
        return confirm(text("ask_after_web"))
