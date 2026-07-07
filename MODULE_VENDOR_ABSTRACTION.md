# Module Vendor Abstraction

VoHive historically treated the managed modem as Quectel-compatible. The first
compatibility boundary is now the AT command dialect selected by
`devices[].module_vendor`.

## Current Dialects

- Empty, `auto`, or `quectel` keeps the existing Quectel behavior.
- `simcom` enables SIMCOM SIM7500/SIM7600 command mappings.

The field is intentionally optional. Existing configurations continue to use the
Quectel dialect and do not need migration.

## Abstracted AT Capabilities

The modem manager resolves an internal `ATDialect` at construction time. The
dialect owns vendor-specific commands and parsers for:

- initialization commands
- SIM inserted status
- ICCID
- LTE serving cell and radio info
- IMS status when supported
- USB network mode when supported
- USB Audio control

Generic 3GPP commands such as `AT+CGSN`, `AT+CIMI`, `AT+COPS?`, `AT+CREG?`,
`AT+CSQ`, `AT+CGDCONT?`, `AT+CMGF`, `AT+CNMI`, `AT+CMGL`, `AT+CMGR`, and APDU
commands remain shared.

## SIMCOM SIM7600CE Notes

The SIM7600CE default USB ID documented by SIMCOM is `1e0e:9001`. The static
discovery path recognizes SIMCOM PIDs from `AT+CUSBPIDSWITCH=?` and prefers USB
interface 2 as the AT port while still detecting the network/control side by
Linux driver capability (`qmi_wwan`, `cdc_mbim`, or WWAN class devices).

SIMCOM support currently maps:

- `AT+CICCID` for ICCID
- `AT+CPSI?` for LTE serving cell, band, channel, RSRP, RSRQ, and SINR
- `AT+CPCMREG` for USB Audio control

`AT+QCFG="usbnet"` remains Quectel-only. SIMCOM PID switching is exposed as a
manual AT template rather than being reused through the existing USBNET endpoint,
because `AT+CUSBPIDSWITCH` changes USB composition/PID and is not equivalent to
Quectel USBNET mode values.

## Next Boundary

The QMI control implementation still imports `github.com/boa-z/quectel-qmi-go`.
Before adding more modem families, wrap that dependency behind a local package
whose public names are vendor-neutral, then move Quectel-specific assumptions
into adapter code. The current `backend.DeviceBackend` and modem `ATDialect`
layers are the first two stable boundaries for that work.
