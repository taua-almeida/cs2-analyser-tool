# Player data

The `--save` flag writes the analysis to a file. Saved JSON is a complete analysis record:

```json
{
  "players": {
    "<steam-id>": { "...": "the player fields below" }
  },
  "teams": [
    { "...": "the two logical teams, see Logical teams" }
  ],
  "map_data": {
    "map_name": "de_mirage",
    "total_rounds": 24,
    "rounds_won_ct": 11,
    "rounds_won_t": 13
  },
  "game_mode": "premier"
}
```

Consumers read players from `.players`, keyed by SteamID, a numeric key that JSON renders as a string. Player selection (`--players` or the interactive picker) limits only `.players`; `teams`, `map_data` and `game_mode` always describe the whole match, from the same parse, so a team roster can reference SteamIDs that selection removed from `.players`. This intentionally breaks the previous saved shape, which was the bare `{ "<steam-id>": ... }` player map now nested under `players`. There is no compatibility mode. See [Match data](#match-data) and [Logical teams](#logical-teams-teams) for the match-level fields.

Each `.players` entry contains all fields below; CSV stays a flat player-only file with a flattened selection of them plus the computed K/D, and carries no match-level columns. The CLI table shows a narrower selection still, with `--details` printing the rest in extra tables below it. Stats never include warmup. Bots are never listed, but kills on bots count like any other kill.

## Fields

### Player top level

| Field | JSON key | Description |
| --- | --- | --- |
| SteamID | `steam_id` | 64-bit SteamID of the player. |
| Name | `name` | Last name the player used in the demo. |
| UserID | `user_id` | Server user ID in this demo. |
| TeamID | `team_id` | Map-local ID of the [logical team](#logical-teams-teams) the player played for, or 0 for a player who never participated in an authoritative scored round. |
| Deaths | `deaths` | Deaths, including suicides, ordinary World deaths such as falls, and C4 deaths. A round-ending C4 blast can emit deaths after `RoundEnd`; they still count during `GamePhaseStartGamePhase`. A C4 death emitted after `RoundEnd` during `GamePhaseGameHalfEnded`, plus match-end World cleanup, is excluded. |
| DeathsTraded | `deaths_traded` | Per-round traded-death count: `{total, ct, t}`. A round contributes once when at least one of the player's deaths was avenged inside the trade window. |

### Kill stats (`kill_stats`)

| Field | JSON key | Description |
| --- | --- | --- |
| Total | `total` | Enemy kills. Teamkills are not included. |
| HeadShots | `headshots` | Enemy kills that were headshots. |
| Precision | `precision` | Headshot ratio: `headshots / total`, from 0 to 1. Shown as a percentage in the table and CSV. |
| WeaponsKills | `weapons_kills` | Kills per weapon name. |
| TradeKills | `trade_kills` | Kills on the enemy who had killed a teammate earlier in the same round, within 5 seconds of that death. See [trade window](#trade-window) for the exact rule. |
| TeamKills | `team_kills` | Kills on teammates. |

#### Trade window

A kill is a trade when it lands on the *specific* enemy who killed a teammate, within 5 seconds of that teammate's death. In detail:

- It must be that killer. Killing any other enemy inside the window is not a trade.
- Both deaths have to fall in the same round.
- One kill gives its player one `trade_kill` and trades at most one death. If the enemy killed several teammates inside the window, the oldest eligible death gets the credit.
- Post-round events deliberately remain eligible: a death after the round is decided can be traded, and a revenge kill after the round can trade an earlier death, as long as both events stay in this round and inside the window. This differs from opening duels, which cannot start after the round is decided.

`deaths_traded` lives at the player top level beside `deaths` because it describes what happened to the player who died, not a kill they made. It is a per-round count, not a per-death count: the tracker records a boolean per player, so a round adds at most one even when a respawn mode lets that player die and be traded more than once. Its CT/T split always uses the side that player held in that round, never the team they finished the match on. No separate rate is stored. `deaths_traded.total / deaths` is only a per-death rate in modes without mid-round respawns; in respawn modes the numerator stays per-round while `deaths` counts every death.

The 5-second window is a deliberate choice, not a value read out of the demo: HLTV describes a traded death as one avenged within five seconds, and Refrag documents the same duration for its Trades column. Events identify server-tick intervals rather than exact instants. The comparison therefore uses the closest possible instants in those intervals: `(revenge tick - death tick - 1) × tick duration <= 5 seconds`. At 64 ticks per second, a delta of 321 ticks is the inclusive boundary; 322 is outside. Relative ticks make this invariant to the absolute demo-clock phase and avoid demoinfocs' float32 timestamp rounding. If demoinfocs cannot provide a positive tick duration, parsing fails instead of silently reporting zero trades. Fractional configured windows are preserved rather than rounded to whole seconds. Other trackers differ — Leetify builds 4-second kill chains instead — so trade counts are not comparable between tools even when the label matches. If you are forking and your league counts trades differently, the window is the `tradeWindow` constant at the top of `cmd/demo_parser/round_tracker.go`.

The count is in JSON, in CSV as `Deaths Traded` plus its CT/T columns, and in the `Trade stats` table that `--details` prints. The main table stays unchanged.

### Assist stats (`assist_stats`)

| Field | JSON key | Description |
| --- | --- | --- |
| Total | `total` | Assists on enemy kills, flash assists included. |
| FlashedEnemies | `flashed_enemies` | Flash assists: the victim was blinded by this player's flashbang when killed. |
| DamageGiven | `damage_given` | Health damage credited against enemies. Team damage, self damage and bomb damage never count (HLTV convention). An attacker-less World event is credited to the enemy who damaged the same victim in the same or immediately preceding demo frame; isolated World damage and match-end cleanup are excluded. |
| ADR | `adr` | Average damage per round: `damage_given / total rounds of the match`. |

### Utility stats (`utility_stats`)

Match-wide use and effectiveness of the six standard purchasable grenades: flashbang, smoke, HE, molotov, incendiary and decoy. Game-mode equipment such as snowballs, breach charges and bump mines is excluded. The first version has no CT/T split, per-round rate or self-flash field.

| Field | JSON key | Description |
| --- | --- | --- |
| EnemiesFlashed | `enemies_flashed` | Enemy `PlayerFlashed` events attributed to this player. This counts events, so flashing the same enemy twice adds two. |
| FriendsFlashed | `friends_flashed` | Teammate `PlayerFlashed` events attributed to this player. Self-flashes are excluded. |
| EnemyFlashTimeSeconds | `enemy_flash_time_seconds` | Sum of new blind time added by enemy flash events, in seconds. When a blinded player is re-flashed, already-covered time is not counted twice. |
| AverageEnemyFlashTimeSeconds | `average_enemy_flash_time_seconds` | `enemy_flash_time_seconds / enemies_flashed`, or 0 when no enemy was flashed. It is derived after parsing so the count and total duration remain the source values. |
| UtilityDamage | `utility_damage` | Actual enemy health removed by damaging utility: `{total, he, fire}`. See [utility damage](#utility-damage). |
| GrenadesThrown | `grenades_thrown` | Uses of standard grenades: `{total, flash, smoke, he, molotov, incendiary, decoy}`. See [grenades thrown](#grenades-thrown). |
| UnusedUtilityValue | `unused_utility_value` | Cumulative purchase value of standard grenades carried at qualifying deaths. See [unused utility value](#unused-utility-value). |

Flash assists remain at `assist_stats.flashed_enemies`; `utility_stats` does not duplicate them. The `Utility effectiveness` table shown by `--details` places that existing count beside the new flash, damage and unused-value fields.

#### Utility damage

Utility damage reuses the same per-event real-health cap as `assist_stats.damage_given`. It never exceeds either the damage reported for the event or the victim's actual health drop. Self damage, team damage and bomb damage are excluded from both totals.

- `he` counts HE grenade damage.
- `fire` combines molotov and incendiary damage. Inferno hurt events do not reliably identify which fire grenade created the flames, although their throw counts remain separate.
- `total` is `he + fire`.
- Gun damage and direct projectile chip damage from otherwise non-damaging grenades do not count.

Smoke and decoy therefore have use counts but no invented effectiveness value.

#### Grenades thrown

Each `GrenadeProjectileThrow` adds one use. `WeaponFire` is not counted, and warmup events are ignored. `total` is the sum of the six named counts. If demoinfocs cannot resolve a projectile model, the analyser recovers the type from the thrower's active inventory item when possible. Smoke and decoy are represented by throws only.

#### Unused utility value

This is a cumulative value across death events, not an average and not the largest inventory seen at one death. Every qualifying event contributes, including repeated deaths in respawn modes, suicides, bomb and fall deaths, and combat deaths after the round result. A C4 event emitted after `RoundEnd` during `GamePhaseGameHalfEnded` still contributes here even though it is excluded from raw player death totals. Automatic World cleanup after the round has already been decided is excluded.

Only standard grenade inventory is valued. Weapons, armour and game-mode equipment do not contribute. Quantity is `AmmoInMagazine + AmmoReserve`, which counts a second flashbang stored as reserve ammo. The pinned Mirage case in `TestProcessDemoGolden` explicitly verifies that the demoinfocs version used by this project still exposes the victim's pre-death inventory during `Kill`, without parsing the fixture a second time.

`Equipment` has no price API, so `cmd/demo_parser/utility.go` owns one explicit price table. It uses normal Competitive buy-menu prices from the August 2026 CS2 economy:

| Grenade | Price |
| --- | ---: |
| Flashbang | $200 |
| Smoke | $300 |
| HE | $300 |
| Molotov | $400 |
| Incendiary | $500 |
| Decoy | $50 |

Every entry is unit-tested. Update the table and this convention together if Valve changes the economy.

All utility scalars are appended to CSV after the existing columns. `--details` adds `Utility effectiveness` and `Grenades thrown` tables; the main table is unchanged.

### Map stats (`player_map_stats`)

| Field | JSON key | Description |
| --- | --- | --- |
| MVPs | `mvps` | Round MVP awards. Event and scoreboard values are committed only for authoritative scored rounds. |
| ACEs | `aces` | Rounds with 5 or more enemy kills. |
| MultiKills | `multi_kills` | Multi-kill rounds bucketed by kill count: `{k2, k3, k4, k5}`. See [multi-kill rounds](#multi-kill-rounds). |
| ClutchesWon | `clutches_won` | Rounds won after being the last player alive on the team against at least one enemy (1v1 counts). Winning by defuse or time counts — no kills required. |
| KAST | `kast` | Percentage of the match's rounds with a Kill, non-flash demo Assist, Survival or Traded death. Flash assists are excluded from classic KAST. `RoundEnd` does not award survival immediately: exit frags and C4 deaths before `RoundEndOfficial` still cancel it. This round/KAST treatment is separate from raw death totals, where a C4 event emitted after `RoundEnd` during `GamePhaseGameHalfEnded` is excluded. Match-end World cleanup does not cancel survival. |
| ApproxEKASTPercent | `approx_ekast_percent` | Approximate eKAST: eco-weighted rating-KAST credits per match round, as a percentage. This is the pre-normalization value behind `rating.kast`, and can exceed 100 because economy weighting makes a qualifying round on a weak buy worth more than 1 — it is never clamped. See [KAST and swing side by side](#kast-and-swing-side-by-side). |
| ApproxRoundSwingPercent | `approx_round_swing_percent` | Approximate round swing: summed modeled round-win-probability deltas per match round, as a signed percentage. This is the pre-floor value behind `rating.round_swing`; a negative match stays negative here. See [KAST and swing side by side](#kast-and-swing-side-by-side). |

#### KAST and swing side by side

Five related values, and none of them interchangeable — three are percentages, two are normalized sub-ratings:

```
classic KAST (%) = qualifying classic rounds / match rounds × 100

Approx. eKAST (%) =
    eco-weighted rating-KAST credits / match rounds × 100

eKAST sub-rating =
    (Approx. eKAST (%) / 100) / 0.79

Approx. swing (%) =
    summed modeled win-probability deltas / match rounds × 100

Round swing sub-rating =
    max(0, 1 + (Approx. swing (%) / 100) × 2.5)
```

- **`kast`** is the classic public KAST definition and the only KAST value in the main table. It is exact, not approximate.
- **`approx_ekast_percent`** is an approximation in the Rating 3.0 style: it adds the [40-damage assist rule](#rating-kast) and weights each qualifying round by the player's buy, so it can exceed 100% and does not claim exact HLTV eKAST parity.
- **`rating.kast`** — displayed and documented as the **eKAST sub-rating** — is the same per-round value divided by the 0.79 baseline, so 1.00 is average. It is not a percentage.
- **`approx_round_swing_percent`** is the signed average modeled win-probability contribution per match round from the [round swing](#round-swing) model, with all of that model's limits; it does not claim exact HLTV Round Swing parity.
- **`rating.round_swing`** — displayed as the **Round swing sub-rating** — scales that value around 1.00 and floors it at 0. It is not a percentage, and the floor means an extreme negative match reads 0 here while the percentage stays visibly negative.

Worked examples: an approximate eKAST of 79.0% is a sub-rating of exactly 1.00, and 100.0% is ~1.27; an approximate swing of +4.0% is a 1.10 sub-rating, −4.0% is 0.90, and a −45% disaster still prints −45.0% while the sub-rating sits at the 0 floor.

Both approximate percentages divide by the whole match's rounds, exactly as ADR and the sub-ratings do, so a late joiner is diluted rather than rated only on the rounds they played. Zero match rounds leave both fields 0. The two are in JSON, appended to CSV as `Approx. eKAST (%)` and `Approx. swing (%)` after all existing columns — the CSV's `Rating KAST` and `Rating Round Swing` columns keep carrying the normalized sub-ratings under their existing names for compatibility — and in the `Approximate Rating 3.0 metrics` table that `--details` prints, with swing explicitly signed. The main table is unchanged.

#### Authoritative scored rounds

Round-derived facts are queued until the scoreboard's `TotalRoundsPlayed`
advances. The queue survives a later round start, so a delayed property update
cannot silently lose a genuine round. If several candidates wait for one
scored-round slot, a round that received `RoundEnd` is used before an unended
same-score setup candidate; unresolved extras are discarded at parse end.
Participation, KAST, survival, rating-KAST, rating kill/damage points, trades,
openings, clutches, multi-kills, aces, round swing, and MVPs all use this gate.
A missing `RoundEndOfficial` is handled at the next round start or parse end,
and duplicate finalization is harmless.

Raw event totals are intentionally separate: kills, deaths, assists, damage,
and utility still reflect the events present in the demo. Filtering a setup
round can therefore change round denominators and round-derived rating facts
without deleting those raw events.

#### Multi-kill rounds

`multi_kills` counts rounds, not kills. A round with three enemy kills adds one to `k3`, and nothing else.

- **Buckets are exclusive.** A 4k is only a 4k, never also a 2k and a 3k. So `k2 + k3 + k4 + k5` is the number of rounds the player got at least two enemy kills in, and the buckets feed the [rating](#rating-rating)'s multi-kill sub-rating directly.
- **Enemy kills only**, the same rule ACEs use. Teamkills never count, so four enemy kills plus a teamkill is a 4k.
- **A round with 0 or 1 kills is in no bucket.** The smallest multi-kill is the 2k.
- **Post-round kills count**, exit frags included, again matching ACEs.
- **`k5` and `aces` are the same rounds by construction.** Both are "5 or more enemy kills" — `k5` takes everything from 5 up rather than exactly 5, so the two can never disagree even in the freak case of a sixth kill after a disconnect and reconnect. The duplication is deliberate: `multi_kills` stays a complete, self-contained bucket set for the rating work, and `aces` stays where it has always been for anything already reading it.

The four counts are in the JSON, in the CSV as the `2K`/`3K`/`4K`/`5K` columns, and in the `Multi-kill rounds` table that `--details` prints. The main table does not show them; it is already wide enough.

### Opening duel stats (`opening_duel_stats`)

The opening duel is the first enemy kill of a round. Teamkills, suicides and world deaths never open a round, and neither does a kill landing after the round is already decided. The table shows opening kills and deaths as the `Entry` column, e.g. `5:3`.

| Field | JSON key | Description |
| --- | --- | --- |
| OpeningKills | `opening_kills` | Rounds opened by a kill from this player: `{total, ct, t}`, split by the side the player was on. |
| OpeningDeaths | `opening_deaths` | Rounds opened by this player's death, with the same side split. |
| OpeningSuccessRate | `opening_success_rate` | Percentage of the player's opening kills whose round their team went on to win. 0 without any opening kills. This is a rate, not a count — some trackers use "opening success" to mean the opening-kill tally instead. |

### Side split stats (`side_stats`)

The same core stats split by the side the player was on. Players swap sides at halftime, so the split is attributed round by round: a kill counts on the side the player held when they got it, never on the team they finished the match on.

| Field | JSON key | Description |
| --- | --- | --- |
| Rounds | `rounds` | Rounds the player was in, `{total, ct, t}`. Counted from the players on a side when live play begins, so picking a side during freeze time counts as playing the round. See [who plays a round](#who-plays-a-round). |
| Kills | `kills` | Enemy kills with the same side split. Teamkills are excluded, exactly as in `kill_stats.total`. |
| Deaths | `deaths` | Deaths with the same side split, suicides and world deaths included, exactly as in the top-level `deaths`. |
| ADR | `adr` | Damage per round on each side: `{ct, t}`. See [side denominators](#side-denominators). |
| KAST | `kast` | KAST percentage on each side: `{ct, t}`, same rule as the match-wide `kast`. |

`kills` and `deaths` always add back up to their match-wide counterparts, so `kills.ct + kills.t == kill_stats.total`.

#### Who plays a round

The roster is read twice: once at the round start, and again when freeze time ends. Freeze time runs after the round has already started, and a player who picks a side inside that window spawns and plays the round like everyone else, so the second read folds them in. Someone who swapped sides in that window plays the round on the side they end freeze time on, which is the side their kills count towards.

The second read never revives anyone. A player who disconnected during freeze time stays counted as having played the round, without survival credit, exactly as a mid-round disconnect is treated.

This also feeds the match-wide `kast`, which is why a freeze-time joiner's KAST rounds are no longer dropped either.

#### Side denominators

`adr` and `kast` are divided by `rounds.ct` and `rounds.t` — the rounds that player actually spent on that side. There is no alternative: sides swap at halftime, so no single match-wide CT round count applies to every player, and a substitute or a late connect plays a different number of rounds than the match has.

The match-wide `assist_stats.adr` and `player_map_stats.kast` keep dividing by the rounds of the whole match, following HLTV convention. **The two only reconcile for a player who was there for every round.** For anyone else, the per-side numbers are the higher, more flattering ones, because they are not diluted by the rounds the player missed. One of the test fixtures has a player who joined for the last 3 of 8 rounds: their match ADR is 59.6 and their T-side ADR is 159.0, and both are correct under their own rule.

`adr` and `kast` are 0 for a side the player never played, not null — a `rounds.ct` of 0 gives an `adr.ct` of 0.

Unlike the counts, these two are rates and so have no `total` field. The match-wide value is the existing `assist_stats.adr` / `player_map_stats.kast`.

### Rating (`rating`)

An HLTV Rating 3.0-style rating. **HLTV's exact formula is proprietary, so this is an approximation**: it follows the structure HLTV has described publicly — six sub-ratings, most of them economy-adjusted — with every constant chosen here and calibrated so an average player lands near 1.00. Do not expect it to match the number HLTV publishes for the same match. This section is the field reference; the full calculation — every formula, constant, table and worked example, plus the current status against HLTV's own Rating 3.0 — is in [RATING](./RATING.MD).

| Field | JSON key | Description |
| --- | --- | --- |
| Value | `value` | The blended rating. 1.00 is an average performance, ~1.30 a very strong map, below ~0.70 a poor one. |
| Kills | `kills` | Eco-adjusted kill points per round, against the baseline. |
| Damage | `damage` | Eco-adjusted damage per round, against the baseline. |
| Survival | `survival` | Eco-weighted rounds survived per round, against the baseline. |
| KAST | `kast` | Eco-weighted rating-KAST rounds per round, against the baseline. Displayed as `eKAST sub-rating`; the pre-baseline percentage is `player_map_stats.approx_ekast_percent`. See [rating KAST](#rating-kast). |
| MultiKill | `multi_kill` | Multi-kill points per round, against the baseline. |
| RoundSwing | `round_swing` | Round-win-probability swing per round, scaled around 1.00. Displayed as `Round swing sub-rating`; the signed pre-floor percentage is `player_map_stats.approx_round_swing_percent`. See [round swing](#round-swing). |

Every sub-rating is normalized so 1.0 is average on that axis, and `value` is their weighted blend: kills 25%, damage 20%, round swing 20%, KAST 15%, survival 10%, multi-kills 10%. HLTV does not publish its weights; these follow the emphasis of their Rating 3.0 announcement. All constants named below live in `cmd/demo_parser/rating.go`.

Like ADR and KAST, the per-round averages divide by the rounds of the whole match, so a player who joined late is diluted on every axis rather than rated on only the rounds they played.

#### Equipment tiers and eco adjustment

Every kill and damage event is priced by the duel's economic matchup. Each player's loadout is bucketed at kill or hurt time into one of six tiers by the strongest weapon they carry: sniper, tier-1 rifle (AK, M4s, AUG, SG 553), tier-2 rifle (FAMAS, Galil, SSG 08), SMG/shotgun/MG, upgraded pistol, starter pistol (Glock, USP-S, P2000). A knife-only player counts as a starter pistol. Per-tier strengths give the expected duel win rate, and points scale inversely with it: an even duel is worth 1.10 kill points — the value HLTV quotes — a tier-1 rifle kill on a starter pistol ~0.54, and the reverse upset ~1.66. Damage is scaled by the same factor. The duel is priced loadout against loadout: a rifler's knife kill is still a rifle-tier duel.

Survival and KAST credit use a per-round weight from the player's own buy, captured when freeze time ends: surviving or staying useful on an eco (weight ~1.5 for a starter pistol) counts for more than doing it with a tier-1 rifle (weight 1), and slightly more than with an AWP (~0.95).

#### Rating KAST

The rating's KAST axis differs from the classic `player_map_stats.kast` in its assist rule. Classic KAST counts non-flash demo assists. Rating KAST keeps every demo assist, including flash assists, and additionally credits any player who dealt at least 40 damage to an enemy who died that round — Rating 3.0's threshold, raised from the 25 of Rating 2.0 — whoever or whatever finished them. Kill, survival and traded-death facts are shared. The classic KAST field itself remains separate from this approximate rating logic.

The eco-weighted credits per match round are exposed before the baseline division as `player_map_stats.approx_ekast_percent`; the normalized `rating.kast` — the eKAST sub-rating — is that percentage over 100 divided by the 0.79 baseline. See [KAST and swing side by side](#kast-and-swing-side-by-side).

#### Round swing

Round swing measures how much each kill moved the round towards being won. A win-probability model — a lookup table over both sides' alive counts, with a planted bomb applied as a log-odds shift towards the T side — is evaluated before and after every kill; the killer gains the difference and the victim loses it, so swing sums to zero across each round and, per round, across each 5v5. Diminishing returns are built in: the fifth kill of a wipe moves an almost-decided round far less than an opening kill moves an even one. Post-round kills move nothing. In this first version suicides, teamkills and world deaths move no swing either, and neither does planting or defusing itself — only kills.

The sub-rating is `1 + swing per round × 2.5`, floored at 0. An average player sits at exactly 1.00 by construction, because the swing they gain is swing an opponent lost.

The swing per match round is exposed before that normalization as the signed `player_map_stats.approx_round_swing_percent`, which keeps the negative information the floor discards. See [KAST and swing side by side](#kast-and-swing-side-by-side).

#### Calibration

The per-round baselines that define "average" (0.69 kill points, 70 eco-damage, 0.37 eco-weighted survivals, 0.79 eco-weighted KAST rounds, 0.22 multi-kill points — multi-kill rounds score 1/2/4/7 points for a 2k/3k/4k/5k) were measured over five real matches: the three integration fixtures plus one HLTV pro match and one FACEIT match. Over that set the rounds-weighted mean rating is 1.000 and each sub-rating mean is within 2% of it. The set is small and skews towards standard competitive play; ratings in respawn or wingman modes are computed but not meaningful, since the swing table and the baselines assume 5v5 rounds.

The rating is in JSON, in CSV as `Rating` plus one column per sub-rating, and in the main table as the `Rating` column beside KAST; `--details` prints the full breakdown in the `Rating breakdown` table, with the KAST and round-swing columns headed `eKAST sub-rating` and `Round swing sub-rating`, and the pre-normalization percentages in the `Approximate Rating 3.0 metrics` table beneath it.

## Match data

Saved JSON carries two match-level fields beside `players`. `map_data` is an object:

| Field | JSON key | Description |
| --- | --- | --- |
| MapName | `map_name` | Map the demo was played on. |
| TotalRounds | `total_rounds` | Rounds played. |
| RoundsWonCT | `rounds_won_ct` | Final score of the side finishing as CT. |
| RoundsWonT | `rounds_won_t` | Final score of the side finishing as T. |

`rounds_won_ct` and `rounds_won_t` are final side scores, not logical team identity: `rounds_won_ct` belongs to whichever lineup happened to finish the map as CT. They are kept for compatibility. The mapping from scores to the lineups that earned them lives in [`teams`](#logical-teams-teams), whose per-team scores reconcile with these two fields without depending on final sides.

`game_mode` is a top-level string, e.g. `"premier"`. The primary source is the demo's `ServerInfo.GameSessionConfig`, whose `gamemode` field names the mode directly on Valve servers; a non-empty value there is always reported as-is and takes precedence over everything below.

Tournament servers (the FACEIT, BLAST and ESL demos behind HLTV downloads) send that config with an empty `gamemode`. When the primary value is empty, the parser falls back to the game rules the demo records as ConVars: a demo that explicitly recorded `mp_maxrounds` `24`, `mp_halftime` on and `mp_friendlyfire` on — the regulation MR12 competitive ruleset — reports `"competitive"`. That is the only value the fallback can produce, chosen to match the string Valve servers use for classic competitive play.

Anything less specific stays empty: a demo that names no mode and does not record that full ruleset saves `"game_mode": ""`. An unrecorded ConVar reads as unknown, never as a default, and the mode is never inferred from context outside the demo's own metadata, such as the filename.

## Logical teams (`teams`)

CT and T are temporary sides: lineups swap ends at halftime and with every overtime half. `teams` exports the two *logical* teams of the map — the lineups that persist across those swaps — and each player references theirs through `team_id`:

```json
"teams": [
  {
    "team_id": 1,
    "name": "Rooster",
    "aliases": ["Rooster"],
    "score": 9,
    "roster": [76561198050779850, 76561198081702155]
  },
  { "team_id": 2, "...": "the second team" }
]
```

| Field | JSON key | Description |
| --- | --- | --- |
| TeamID | `team_id` | Deterministic map-local ID. Team 1 played CT in the seeding round, team 2 played T. See [ID scope](#team-id-scope). |
| Name | `name` | Display name: the alias observed in the most accepted rounds, ties broken by first observation. `""` when the demo never showed a nonempty clan name — a name is never invented. |
| Aliases | `aliases` | Deduplicated nonempty clan names observed while the team held a side, in first-observation order. |
| Score | `score` | The team's accepted round wins. See [team scores](#team-scores). |
| Roster | `roster` | Every eligible SteamID64 that played an accepted round for the team, substitutes included, sorted ascending. |

Serialization is deterministic: teams are ordered by `team_id`, rosters are sorted, and aliases keep observation order.

### Identity evidence

Identity is seeded once, from the first authoritative scored round (the same [gate](#authoritative-scored-rounds) every round-derived stat uses) that has at least one eligible participant. Warmup, pregame knife rounds, restarts and unscored setup rounds can therefore never seed a team, and neither can rounds played only by bots. Eligible means a real competitor: nonzero SteamID64, not a bot, not a coach, not a spectator.

A round's team-roster evidence is the players on a side when live play begins — the end of freeze time — not the round-start snapshot the player stats keep. The difference matters at halftime and in overtime: the side switch can land during freeze time, after the round started, and a player who disconnects inside that window would otherwise be presented on their stale pre-switch side, among the other lineup. Such a leaver keeps their player-stat participation for the round but contributes no team facts to it; their membership from earlier rounds is untouched.

Team facts are also resolved once at parse end rather than while streaming, because a setup round that received `RoundEnd` and then restarted at the same score is indistinguishable in the moment from a genuine round whose authoritative count replicated late. The parse-end queue reveals which shape occurred: decided rounds left without a scored slot mean an equal number of the earliest decided candidates were setup restarts, and exactly that many are excluded from team identity, aliases and scores. Player statistics keep their existing streaming gate unchanged.

From then on every accepted round's two side rosters are mapped back to the seeded teams through shared SteamIDs — the strongest identity evidence a demo offers. A mapping is *confident* when it has roster evidence (at least one shared eligible SteamID) and no conflict (no player it would place on both teams). Substitutes resolve through their teammates and join the uniquely matched team; a reconnecting player is already a member and cannot be duplicated; even a side of entirely new players resolves by elimination when the other side maps uniquely.

Anything less confident is an *ambiguity* and fails the parse with an error carrying the competing evidence — the sides' rosters, the overlap with each team, and both teams' members — instead of guessing. In particular, a SteamID that appears to participate for both logical teams is an explicit error, never a silent `team_id` overwrite.

The following are never identity: sides (current or final), clan names, `TeamState.ID()`, display names, user IDs, entity IDs, filenames, map names.

### Clan names are labels

Clan names corroborate identity but never define it. They are captured per side per round, follow the side entities across swaps, and attach to whichever logical team held that side. A team whose clan name changes mid-match keeps every nonempty spelling in `aliases`; `name` deterministically picks the most-observed one (first observation wins ties). An empty clan name stays unknown — no organization name is fabricated — and two teams sharing the same clan name remain two teams, because names never merge identity.

### Team scores

A round win is attributed to a logical team only when the authoritative scored-round gate accepted the round, the demo recorded its winning side, and that side's roster resolved to the team. Each team's `score` therefore equals its accepted round wins, and the two scores reconcile with `map_data.rounds_won_ct`/`rounds_won_t` — which stay exactly as before for compatibility — without ever deriving a team's score from the side it finished the map on. Rounds with no eligible participants (bots only) move no team score; their wins still appear in the side-based `map_data` fields, so the two views can differ in such demos.

### Team ID scope

`team_id` is scoped to one map: the numbering says only which lineup started the seeding round on which side of *this* demo. It makes no claim about organization identity across demos — the same organization can be team 1 on one map and team 2 on the next. Matching teams across the maps of a BO3/BO5 series is issue #34's job, deliberately not this field's.

Players keep their existing CT/T side splits (`side_stats`), which remain attributed round by round and are unaffected by logical team identity. A player with `team_id` 0 exists in the demo's raw events but never played an accepted round (for example, someone only present during an unscored setup round).

## Series analysis (BO3/BO5)

`analyse --best-of 3|5` with repeated `--demo` flags aggregates a *completed* series. The format is always explicit: multiple demos without `--best-of` are rejected, a series is never inferred from the file count, and history never infers one either. The demos are the played maps in order, and that order is preserved everywhere — parsing, output, errors and JSON. Before parsing, every file is streamed through SHA-256; two inputs with the same digest are rejected as the same demo regardless of their paths.

### Completed series only

A BO3 accepts exactly 2 or 3 played maps and a BO5 exactly 3, 4 or 5. Each map must be decided: exactly two logical teams, untied per-team scores that add up to the map's rounds, and players referencing only those teams. Only the competitive 5v5 format is supported, enforced three ways: a map reporting any game mode other than `competitive`, `scrimcomp5v5`, `premier` or the unknown `""` is rejected outright (casual, deathmatch and Wingman's `scrimcomp2v2` all fail here, whatever their lobby size); every logical team must have fielded at least five eligible players, rejecting Wingman-sized lineups before identity resolution — substitutions only ever make a roster larger, so a competitive map with stand-ins always passes; and a map whose per-round participation exceeds what a 5v5 lobby can produce (ten players per round, with one spare for a freeze-time swap) is rejected, which catches oversized lobbies whose demos name no mode. The final supplied map must be the first point at which a team reaches the clinch threshold — 2 map wins in a BO3, 3 in a BO5. An unclinched series and a map supplied after the clinch are both errors; live or unfinished series are out of scope.

### Series team identity

`team_id` inside one map is map-local (see [Team ID scope](#team-id-scope)); the series introduces *series-local* team IDs 1 and 2, seeded deterministically from the first map's team order, that are scoped to this one series and claim nothing beyond it. Every map's `team_assignments` records the explicit bridge from its map-local IDs to the series IDs.

The two series teams are resolved through a unique joint roster assignment. Each map after the first can keep the first map's orientation or cross it; all combinations (at most 2^4 for a BO5) are evaluated jointly — never greedily map by map — under one rule: the union rosters of the two series teams must stay disjoint. Exactly one surviving combination is success. Zero is a mixed/conflicting-team error, several is an ambiguity error; both are structured, `errors.As`-testable values carrying the map indexes, map-local team IDs, rosters and competing assignments. The joint evaluation is what lets a middle map played entirely by substitutes resolve through a later map that ties those substitutes back to a lineup.

Substitutions are roster additions to the same series team, never a new team. Sides, map-local IDs, filenames, map names, scores, player names and clan names are never identity; clan names may corroborate a label but never resolve an otherwise ambiguous assignment. Series team `aliases` are the deduplicated map-level aliases in first map-observation order, and `name` is the most-observed nonempty map-level name, ties broken by first observation.

### Series JSON

`--save` writes the series envelope (CSV behaves differently, see below):

```json
{
  "best_of": 3,
  "winner_team_id": 2,
  "teams": [
    {
      "team_id": 1,
      "name": "Rooster",
      "aliases": ["Rooster"],
      "maps_won": 1,
      "rounds_won": 32,
      "roster": [76561198000000000]
    }
  ],
  "players": {
    "<steam-id>": { "...": "aggregate player, see below" }
  },
  "maps": [
    {
      "sha256": "...",
      "winner_team_id": 2,
      "team_assignments": [
        { "map_team_id": 1, "series_team_id": 2 },
        { "map_team_id": 2, "series_team_id": 1 }
      ],
      "analysis": { "...": "the unchanged standalone map record" }
    }
  ]
}
```

`maps` keeps the supplied order; each entry carries the demo's SHA-256, the map winner translated into series-team terms, the map-local-to-series-team assignments, and the complete standalone analysis with the exact single-map shape documented above. `winner_team_id` at the top level is the completed-series winner, and each team's `maps_won`/`rounds_won` are its map wins and summed logical round wins.

### Aggregate players

`players` is keyed by SteamID64, the only cross-map player identity — names are labels. Each entry is a series-specific record: it deliberately has no `user_id`, because server user IDs are map-local and meaningless across demos. `aliases` collects the player's deduplicated nonempty per-map display names in map order, and `name` picks the most-observed one, first observation breaking ties — the same rule team names use.

Additive facts (kills, deaths, headshots, assists, flash assists, damage, trades, team kills, weapon kills, MVPs, aces, multi-kill buckets, clutches, opening kills/deaths, CT/T count splits, utility counters, flash duration, unused utility value) are exact sums of the per-map values. Every rate is recomputed from exact summed numerators and denominators preserved by the parser — never by combining or averaging the rounded per-map rates:

- `maps_played` counts the maps where the SteamID participated for a logical team, and `rounds` sums those maps' total rounds. All ten players of a three-map series with 22+19+23 rounds therefore have a 64-round denominator.
- Match-wide ADR, classic KAST, the approximate eKAST and round-swing percentages divide by `rounds`, mirroring how a single map divides by the whole map's rounds; the per-map late-join dilution inside each map is unchanged.
- CT/T ADR and KAST divide by the player's own summed CT/T rounds; precision, opening success and average enemy flash duration divide their exact summed numerators by their summed counts.
- `player_map_stats` is named `player_series_stats` in the aggregate record; its fields have the same meaning over the series denominator.

### Series rating

The series `rating` is not an average of map ratings. The parser preserves each map's exact raw rating facts — eco-adjusted kill points and damage, eco-weighted survival and rating-KAST credits, signed round swing, multi-kill buckets — and the rating model runs once over their sums with the player's series round denominator. If a map was not produced by this parser and carries no raw facts (only possible for programmatically constructed inputs, never for `analyse`), the rating is explicitly `null` rather than reconstructed from rounded output.

### Player selection in a series

`--players` is resolved only after cross-map identity and alias collection: names match any collected alias case-insensitively, duplicates count once, a name with no match fails listing the available players, and a name matching more than one SteamID fails with the candidate SteamIDs. Selection never affects team resolution or the aggregate calculations — a filtered save narrows `players` and each map's `analysis.players`, while teams, rosters, scores, map results and team assignments remain complete.

### Series CSV

CSV stays the flat aggregate-player table with the existing single-map header, column order and formatting — no series, team, hash or map columns. The one series-specific cell rule: a player whose series rating was [omitted](#series-rating) has empty cells in the seven rating columns, never a fabricated `0.00`; the CLI tables render the same omission as `-`. Full per-map series data requires JSON.
