DESCRIPTION = "Locks, alarm panels and everything with the label mira_confirm only with a yes."

# DOMAINS and critical() also say what a task may not switch
# (docs/spec/hooks.md): a household critical.star needs both.
# Labels are seen by their id ("mira_confirm"), not by their name ("Mira:
# nachfragen", "Mira: confirm first"), which may be anything.
DOMAINS = ["lock", "alarm_control_panel"]

def critical(e):
    return e.domain in DOMAINS or "mira_confirm" in e.labels

def pre_tool(call, verdict):
    if call.tool.read_only or call.tool.own:
        return None
    if [d for d in call.domains if d in DOMAINS]:
        return confirm(text("critical"))
    for e in call.targets + call.candidates:
        if critical(e):
            return confirm(text("critical"))
