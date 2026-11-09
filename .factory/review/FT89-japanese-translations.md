---
source: pr #89
branch: factory/FT89-japanese-translations
round: 1
---
# i18n(ja): complete and correct Japanese translations

## factory · 2026-11-09 19:52
Imported GitHub PR #89 directly into review at the owner's request: “把 89 放到 review”.
Source: https://github.com/lyricat/inua/pull/89
Upstream head: 3ca8b3235eea20126bdd34cc073d259ff4216f47
Factory branch preserves the upstream head without changes.

Completes the Japanese catalog and corrects mistranslations and context errors including Right, Hide, Hang Up, Quit with Children, MEMORY, Traffic, Up, and Resident.
Review should check translation context, terminology, placeholders, markup, completeness, generated catalog consistency and regressions against the current base.

The author reports 4,920 translated entries, none remaining, regenerated catalogs via make i18n, and passing make build, go test ./internal/i18n and git diff --check. These claims were not independently verified during import.
No review, tests or visual UI checks run during import. Awaiting review; not approved for merge.
