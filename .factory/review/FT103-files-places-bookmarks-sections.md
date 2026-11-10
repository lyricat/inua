---
branch: factory/FT103-files-places-bookmarks-sections
round: 1
---
# Files: separate Places and Bookmarks sidebar sections

## factory · 2026-11-10 16:26
Registered from the owner's Files UI request. Backlog only; no build started.

User request: "split \"Places\" into \"Places\" and \"Bookmarks\""

- Show built-in locations under Places and user-created bookmarks under a separate Bookmarks heading.
- Preserve existing bookmark targets, ordering, persistence and supported management actions; do not duplicate or lose entries during the change.
- Specify empty-section behavior in the build plan. Check adding/removing bookmarks, restart persistence, navigation and translated headings.

## factory · 2026-11-10 16:28
Moved from backlog to new at the owner’s request: "ok. move them to new". Queued for build; no development started in this board update.

## factory · plan · 2026-11-10 17:05
Behaviour: Home and existing XDG folders remain under PLACES, with Trash except in the picker. Bookmark-kind entries appear under BOOKMARKS before DEVICES. Hide the heading when no displayed bookmarks remain, including first run and the last removal. Preserve existing deduplication, ordering, labels, path/URI targets, navigation, context menus, refresh and errors. Viewing never writes or migrates bookmarks; unreadable or missing files retain empty handling.

Scope: sidebar rendering, accurate comments/documentation and translation catalogs only. No persistence redesign, preference or unrelated UI.

Decisions: reuse existing Place.Kind and shared kicker styling; retain built-in deduplication and put Trash in Places. Alt-number shortcuts follow the final visible order.

Checking: make i18n and make test; existing bookmark tests cover ordering, deduplication and add/remove. Manual checks pending: empty/nonempty sections, add/remove including last bookmark and multiple windows, restart persistence, navigation and Alt-number, translations and picker without Trash. UI is not checked by eye here.

## factory · 2026-11-10 17:05
Built in 2f9560e3 (feat(files): bookmarks get their own sidebar section), pushed to factory/FT103-files-places-bookmarks-sections. Places and Bookmarks are separate; an empty Bookmarks section is hidden. Existing bookmark storage and management are unchanged. Updated docs and six translated headings/catalogs; make i18n is reproducible.

Installs: none. Coder reports go build ./... and make test passed; manager independently reran make test successfully with a throwaway HOME and isolated XDG directories. No protected paths changed; one item-scoped commit from a6787817.

UI changed and was not checked by eye. Manual checks listed above remain pending. Initial branch push needed command-scoped credential.useHttpPath=true; retry succeeded, without changing user configuration.

Moved new/ → review/ for review round 1.
