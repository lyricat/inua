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
