---
branch: factory/48-device-battery-levels
round: 0
---
# Battery levels of connected devices

## lyric · 2026-11-01 12:47
Show the battery level of every connected device. Today only the
keyboard's battery level shows.

## lyric · 2026-11-01 12:49
For example, the Magic Trackpad and the WHOOP 5.0: their battery levels
don't show in the Bluetooth devices list. Show the power level for every
rechargeable device there.

## factory · plan · 2026-11-01 12:50
Behaviour: in the Bluetooth devices list, a connected device shows
"Connected, battery N%" whenever its level is known, as the keyboard does
today. The level comes from BlueZ (org.bluez.Battery1) as now, and, when
BlueZ has none, from the UPower device whose serial is the device's
address. That is how the kernel's HID drivers report it: the Magic
Trackpad is hid-<address>-battery in UPower, a touchpad at 2% on this
machine, with no Battery1. The level follows UPower's changes while the
list shows. A device with no level from either still shows "Connected".
Scope: internal/system/bluetooth.go (read and watch UPower's devices,
match them by address), its tests. The list's wording stays. Left out:
devices that report only through their own protocol, and low-battery
warnings.
Decisions: BlueZ's level wins when both have one, since it is the
device's own report. Matching is by address, case-insensitive, because
UPower's serial for these devices is the address in lower case. UPower
rather than reading /sys directly: power.go already uses it, and it
sends changes. The WHOOP 5.0 was not connected during this analysis: it
shows a level if it reports through BlueZ's battery service or a kernel
driver, and none otherwise.
Checking: tests on the private D-Bus of internal/dbustest with a fake
BlueZ and a fake UPower: a device with only a UPower level shows it, one
with both shows BlueZ's, an unmatched UPower device changes nothing, and
a UPower change updates the level. By hand: the Bluetooth page lists the
Magic Trackpad with its percentage.

## factory · 2026-11-01 12:58
Built on factory/48-device-battery-levels (be5a4e8): a connected device
with no BlueZ level takes the level of the UPower device with its
address, and the list follows UPower's changes. BlueZ's level wins when
both have one. make test passes; new tests on a fake BlueZ and a fake
UPower cover a UPower-only level, BlueZ winning, an unmatched UPower
device, a level change and a device added later.
Not checked: the Bluetooth page by eye in a nested session, a real Magic
Trackpad, and the WHOOP 5.0, which was not connected; it shows a level
only if BlueZ or a kernel driver reports one.
