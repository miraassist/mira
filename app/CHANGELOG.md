# Changelog

The app has the version of Mira's release; what changed is in the release notes:
<https://github.com/miraassist/mira/releases>

## 0.12.0

- Hooks: rules of your own as `.star` files in the folder `hooks` next to `mira.yaml` (this app's config folder).
- `security.confirm_domains`, `confirm_entities` and `confirm_tools` in `mira.yaml` are no longer read; use the label
  "Mira: nachfragen" in Home Assistant or a hook.

## 0.11.0

- New option **Reset data**: deletes Mira's data at the next start and keeps `mira.yaml`.
- Pairing with another Home Assistant takes the code from this app's log.

## 0.10.0

- First version as a Home Assistant app.
