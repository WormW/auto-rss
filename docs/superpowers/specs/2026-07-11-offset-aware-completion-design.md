# Offset-Aware Episode Completion Design

## Problem

`current_episode` and `latest_episode` store the original episode number parsed from RSS or download records. `total_episodes` stores the number of episodes in the selected season, while `episode_offset` maps original episode numbers into that season.

Completion and progress currently compare the original episode number directly with `total_episodes`. A subscription with offset 170 and 52 episodes is therefore marked complete as soon as its original episode number is at least 52, instead of when it reaches episode 222.

## Episode Semantics

The existing storage contract remains unchanged:

- `current_episode`: highest collected original episode number.
- `latest_episode`: highest known original episode number.
- `episode_offset`: number subtracted from an original episode number to obtain its season-relative number.
- `total_episodes`: number of episodes in the season.

For status and display calculations:

```text
relative_episode = max(0, original_episode - episode_offset)
completed = total_episodes > 0 && relative_current_episode >= total_episodes
```

For offset 170 and total 52, original episode 221 is season episode 51 and remains in progress. Original episode 222 is season episode 52 and is complete.

## Implementation

Add model-level helpers that calculate relative current/latest episode numbers and completion status. Backend consumers use these helpers for smart-fetch decisions and calendar completion checks. The statistics SQL applies the equivalent expression so database counting matches the model behavior.

The subscriptions UI uses a shared local relative-episode calculation for completion tags, progress percentages, season completion styling, and date/year fallback checks involving `latest_episode`. Raw episode numbers remain available for download identity and collection filtering.

No database migration or stored episode-number rewrite is required.

## Existing Incorrect State

`completed_at` may already have been populated by the old calculation. When a subscription is evaluated and is no longer complete under the corrected calculation, smart fetch clears `completed_at`. This prevents an old false-completion timestamp from immediately triggering the completed-age stop policy when the subscription eventually reaches its actual final episode.

## Tests

Regression coverage will verify:

- Offset 170, total 52, current 221 is not complete.
- Offset 170, total 52, current 222 is complete.
- Offset zero retains the existing completion behavior.
- Smart fetch clears a stale `completed_at` for an offset subscription that is not actually complete.
- Repository completion statistics use offset-aware completion.
- Frontend completion and progress calculations use relative episode numbers through the normal frontend test/build checks available in the repository.

## Scope

This change does not alter RSS parsing, download episode identity, file naming, missing-episode collection, or stored database values. It only makes completion and progress consumers respect the established offset mapping.
