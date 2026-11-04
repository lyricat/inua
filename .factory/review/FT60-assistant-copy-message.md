---
branch: factory/FT60-assistant-copy-message
round: 1
---
# Copy a message in the assistant

## lyric · 2026-11-04 22:24
I can't copy a message in the AI assistant. I want to copy the messages.

## factory · plan · 2026-11-04 23:28
Behaviour: Each user message and assistant answer has a Copy action for its current full plain text, preserving Unicode and newlines. Empty conversations have no actions; waiting empty answers cannot be copied. Streaming copies current content, displayed errors remain copyable, and clipboard availability errors appear beside the message. Reset drops old widget references.

Scope: Assistant message UI, shared error styling, documentation and translation catalogs. No conversation export or clipboard-service changes.

Decisions: Use the standard GDK clipboard, shared keyboard-accessible small outlined buttons and one callback. Keep text selectable; no external clipboard commands.

Checking: Run make test with isolated HOME and XDG directories. Pure UI interactions get documented manual checks rather than new automated tests: both roles, multiline/Unicode, streaming, empty/error states, keyboard navigation and reset.

## factory · 2026-11-04 23:28
Recovered the clean item worktree and its existing implementation commit from a stopped build. Codex verified the implementation and fixed clipboard error text colouring.

Built: Per-message Copy buttons, selectable user text, streaming-aware availability, local clipboard errors and reset/shutdown cleanup. Tool and approval rows stay before the answer. Documentation lists manual checks.

Commits: c1b3ec7636042580052ed2f820e315943aa05a42; b901c38bb32b1581226582ef53dd63eaaa703901.
Decisions: Standard GDK clipboard and shared callback; no new automated UI tests, following repository guidance.
Installs: None.
Tests: Codex make test and assistant build passed; make i18n produced no differences. Manager independently ran make test with throwaway HOME/XDG directories: passed. No protected paths changed.
Not checked: UI was not checked by eye; real clipboard pasting, keyboard interactions and the other checks in docs/assistant-copy.md remain for manual verification.
Moved new/ → review/ for review; not merged into master.
