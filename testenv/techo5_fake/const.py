"""Constants of the fake TECHO5 devices."""

DOMAIN = "techo5_fake"
# The actions of an ESPHome device live in the esphome domain, named
# <node>_<action>; the fakes register theirs there too.
ESPHOME = "esphome"
# (kind, device name, node); the node is the device name as ESPHome spells it.
DEVICES = (("show", "Fake Show", "fake_show"), ("echo", "Fake Echo", "fake_echo"))
