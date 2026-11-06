# Pass light app: default row-by-row editor and password generator

## factory · 2026-11-06 17:28
Requested by the owner:
> New factory feature: pass light app, update an alternative edit mode (default), row by row. And provide password generator button (icon button)

Feature requirements:
- Add an alternative row-by-row editing mode to the Pass light app and make it the default editor.
- Keep the existing editing mode available as an alternative.
- Provide a password-generator icon button alongside the password field, with a tooltip and accessible label.

Implementation defaults and acceptance checks:
- Represent the password and subsequent entry lines as editable rows, preserving their order and content; opening and saving an unchanged entry must not lose information.
- Switching editing modes must preserve unsaved changes. Preserve the existing save/cancel behavior and storage format.
- Generate passwords with a cryptographically secure random source. Insert the generated value into the editor without automatically saving or copying it; do not silently overwrite an existing password without an explicit user action.
- Handle empty/new entries, multiline entries, and generation failures without losing edits. Never log password values.
- Add regression coverage for row serialization, mode switching, and password generation. Follow the existing app's UI conventions.

Queued for a factory build; not implemented yet.
