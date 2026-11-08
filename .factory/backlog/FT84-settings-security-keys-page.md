# Settings: a Security keys page for the FIDO2 key itself

## factory · 2026-11-08 14:25
Added to backlog at the owner's request in chat: "is there a manage page for yubikey?", then "add to backlog".

Proposal from chat: Pass's Security keys dialog (FT83) manages only inua's enrolments. A Settings page would manage the security key plugged in:
- Its name and firmware, whether a PIN is set, and the PIN tries left (`fido2-token -I`).
- Set or change the PIN (`fido2-token -S` / `-C`), asked in inua's dialog.
- The gpg keys it unlocks (FT83's enrolments), with a way to Pass to add or remove.

Left out on purpose: listing resident credentials (passkeys; needs the PIN) and reset (wipes every passkey on the key).

Depends on FT83 (`internal/securitykey`).
