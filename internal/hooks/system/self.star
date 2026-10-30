DESCRIPTION = "Rules are changed only by someone Mira knows, always with a yes, never after third-party text, and never a system rule or critical.star. Tasks are changed only by someone Mira knows."

TOOLS = ["hook_save", "hook_switch", "hook_delete"]
PROTECTED = ["rights.star", "background.star", "web.star", "self.star", "ask.star", "critical.star"]

def pre_tool(call, verdict):
    # A change of a task: what it reaches is judged by critical.star.
    if call.tool.name == "task_edit":
        if not call.who.person:
            return deny(text("tasks_unknown"))
        return None
    if call.tool.name not in TOOLS:
        return None
    if not call.who.person:
        return deny(text("rules_unknown"))
    # A web page or a mail must not slip in a rule or switch one off.
    if call.turn.foreign:
        return deny(text("rules_foreign"))
    name = str(call.args.get("name", "")).lower()
    if not name.endswith(".star"):
        name += ".star"
    if name in PROTECTED:
        return deny(text("rules_protected"))
    return confirm(text("rules_confirm"))
