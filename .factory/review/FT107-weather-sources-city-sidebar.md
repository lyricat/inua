---
branch: factory/FT107-weather-sources-city-sidebar
round: 1
---
# Weather: alternative data source and city sidebar

## factory · 2026-11-13 18:15
Requested by the owner:

> new feature request:
> an alternative source for weather app.
> user can switch sources from the more menu button as a dropdown (should fit our bubble design)
> use a left sidebar to display multiple city list instead of the dropdown.
> put it into new/

Requirements:
- Keep the existing weather source and add at least one alternative weather data provider. Choose a suitable provider during implementation and document the choice and any access requirements.
- Let users switch weather sources from a dropdown opened by the weather app's More menu button. Match the existing bubble design, including selected, hover and focus states.
- Replace the city-selection dropdown with a left sidebar listing the user's cities. Selecting a city displays its weather; retain existing city-management functionality.
- Preserve the selected source and city across restarts. Switching source or city must not show stale responses as if they belonged to the new selection.
- Handle loading, empty city lists, unavailable providers and network errors clearly, without silently changing the selected source.
- Add regression coverage for source switching, city selection and saved preferences; follow existing localization and accessibility conventions.

Scope: register this feature in new/ for implementation; no implementation or review requested in this update.

## factory · plan · 2026-11-13 18:48
Behaviour: Restore saved city/source; old preferences default to Open-Meteo. Keep an always-visible city sidebar with search/add/remove and units. More opens a shared bubble menu for source selection. Switching city, source or units invalidates pending requests and replaces old weather with loading. Handle empty cities, unknown sources, network and provider failures explicitly, without automatic source substitution.

Scope: Weather provider/state logic, Weather window/layout/styles, regression tests, generated UI, localization and provider/privacy documentation. Preserve weather.Get defaults for desk/widget callers. No unrelated app changes.

Decisions: Add wttr.in, an independent public no-key coordinate-based provider. Keep Open-Meteo geocoding; show the alternative provider's actual three-day, three-hour cadence rather than promising seven days. Reuse shared index/menu controls and document provider access and limitations.

Checking: Fake HTTP provider fixtures; unit/time handling and malformed/error replies; old/new saved preferences; city selection/removal; stale response invalidation and source/unit-scoped sidebar temperatures. Run make generate, make i18n and make test with a throwaway HOME. UI appearance is not checked by eye here.

## factory · 2026-11-13 18:48
Built and pushed commit a147f3f1 on factory/FT107-weather-sources-city-sidebar.

Added wttr.in alongside Open-Meteo, selected from the More bubble menu; replaced the old city panel/arrows with a persistent city sidebar. Saved source/city, source-specific errors, explicit unknown-source recovery, request invalidation, scoped temperature caches, honest forecast headings and missing polar sunrise/sunset display are covered. Updated generated UI, catalogs, app/architecture documentation and weather privacy disclosure.

Decisions: wttr.in needs no API key and supplies three days in three-hour intervals; documentation records rate/access limits. City search stays on Open-Meteo; desk/widget default weather.Get behaviour remains unchanged. Unknown saved providers are preserved until explicit user choice.

Installs: none.

Tests: make generate and make i18n completed by coder. make test passed with throwaway HOME, independently rerun by manager (Python session tests, go vet, Go race tests). Commit range contains only this item; protected paths unchanged; worktree clean.

Not checked: UI appearance by eye and live requests to either provider. Coder performed a private headless Broadway startup smoke check only. Owner should check sidebar keyboard navigation, More-menu selected/hover/focus states, source/city persistence, units, last-city removal and retry/error appearance in a nested session.

Push note: first credential-helper attempt lacked a repository path; retry with command-local credential.useHttpPath=true succeeded.

Moved new/ → review/, round 1.
