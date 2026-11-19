DESCRIPTION = "The tools a skill names under confirm only with a yes."

# Separate from critical.star (docs/spec/hooks.md): a household that replaces
# critical.star for its house keeps the yes a skill asks for. Replacing this
# file is how a household drops it, on purpose.
def pre_tool(call, verdict):
    if call.tool.confirm:
        return confirm(text("critical"))
