DESCRIPTION = "An unknown voice unlocks nothing."

def pre_tool(call, verdict):
    if call.who.person:
        return None
    for e in call.targets + call.candidates:
        # Home Assistant's intents: HassTurnOff on a lock unlocks it.
        if e.domain == "lock" and call.tool.name == "HassTurnOff":
            return deny("Unlocking only when I know who is speaking.")
