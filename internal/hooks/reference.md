# Writing a rule (a hook)

A rule is a small Starlark file (Starlark reads like Python). Mira calls its function `pre_tool` before every tool
call of the model. It answers whether the call may run.

```python
DESCRIPTION = "One sentence: what the rule does."    # required, in the household's language

def pre_tool(call, verdict):                          # required
    ...
    return None                                       # no objection
```

## Answers

- `None` or `allow()`: no objection.
- `confirm(reason)`: the call needs the user's yes; `reason` is optional.
- `deny(reason)`: the call is not run; Mira reads `reason` and says it in her own words. Write it in the household's
  language, short ("Aufschließen nur, wenn ich weiß, wer spricht.").

The strictest answer of all rules wins (deny over confirm over allow). A rule cannot loosen anything: it can only
make Mira stricter. Rules run before Mira's own; the system has the last word.

## What `call` holds (read only)

| Field | Meaning |
|---|---|
| `call.tool.name` | the tool without its server: `HassTurnOn`, `HassTurnOff`, `HassLightSet`, `HassSetVolume`, `GetLiveContext`, `memory_save`, `message_send`, `radio_play`, … |
| `call.tool.read_only` | the tool only reads |
| `call.tool.own` | one of Mira's own tools (memory, messages, timers) |
| `call.args` | the arguments as a dict (`call.args.get("name")`) |
| `call.domains` | domains the arguments name (`domain`) |
| `call.targets` | entities the call surely reaches: `e.id`, `e.domain`, `e.name`, `e.area`, `e.area_name`, `e.labels` |
| `call.candidates` | entities it could reach (a floor, a domain alone, a name that resolved to nothing); look at both |
| `call.who.person` | the person's key, `""` when nobody is known |
| `call.who.name` | how Mira names them |
| `call.who.by` | `signed_in`, `voice`, `alone`, `conversation`, or `""` |
| `call.where.channel` | `voice`, `telegram`, `background`, … |
| `call.where.area` | the area id of the satellite of a voice turn |
| `call.turn.text` | what the user said in this turn |
| `call.time.hour`, `.minute`, `.weekday` (1 = Monday), `.date` ("2026-10-28") | now, local |

`verdict.kind` is what the rules before decided: `"allow"`, `"confirm"`.

## Facts of Home Assistant

- **Locks**: `HassTurnOn` locks, `HassTurnOff` unlocks (domain `lock`).
- **Covers**: `HassTurnOn` opens, `HassTurnOff` closes.
- **Volume**: `HassSetVolume` with `volume_level` in percent (0–100).
- **Labels by id, never by name**: Mira's label "Mira: nachfragen" is `"mira_confirm"` in `e.labels`.
- **Persons by key** (`call.who.person`), as the house below lists them; areas by id.

## Limits

No `load()`, no files, no network, no clock but `call.time`. A rule that fails counts as deny for every call it
fails on, so keep it simple and handle missing values (`call.args.get("x", "")`).

## Example

```python
DESCRIPTION = "Ohne bekannte Stimme schließe ich nichts auf."

def pre_tool(call, verdict):
    if call.who.person:
        return None
    for e in call.targets + call.candidates:
        if e.domain == "lock" and call.tool.name == "HassTurnOff":
            return deny("Aufschließen nur, wenn ich weiß, wer spricht.")
```

```python
DESCRIPTION = "Im Kinderzimmer nach 20 Uhr keine laute Musik."

def pre_tool(call, verdict):
    if call.tool.name != "HassSetVolume" or call.where.area != "kinderzimmer":
        return None
    if call.time.hour >= 20 and call.args.get("volume_level", 0) > 30:
        return deny("Nach 20 Uhr im Kinderzimmer höchstens 30 Prozent.")
```
