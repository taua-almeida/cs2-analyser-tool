# Rating methodology

This document is the full calculation reference for the `rating` field: every formula, every constant and where it came from. The field-by-field output reference lives in [PLAYER_DATA](./PLAYER_DATA.MD#rating-rating); the constants themselves live in `cmd/demo_parser/rating.go`.

## Which HLTV rating this targets, and how closely

The model targets **HLTV Rating 3.0**, the 2025 successor to Rating 2.1. HLTV has published the structure — six sub-ratings, most of them economy-adjusted, with Impact replaced by a multi-kill rating and round swing — plus a handful of example values, but **the exact formula is proprietary**. This tool implements the published structure with its own constants and makes no claim of matching the number HLTV prints for the same match.

Current status against HLTV's description:

| Rating 3.0 component | Status in this tool |
| --- | --- |
| Eco-adjusted kills | Approximated. Six loadout tiers; kill points scale inversely with the expected duel win rate, anchored on HLTV's published example values. |
| Eco-adjusted damage | Approximated. Same duel factor as kills, applied per hurt event. |
| Eco-adjusted survival | Approximated. Survival weighted by the player's own buy; the direction (eco survival counts more) is our assumption, HLTV has not published theirs. |
| Eco-adjusted KAST | Approximated. Demo assist events plus the 40-damage assist rule, weighted like survival. |
| Assists from 40 damage | Implemented for the rating's KAST. Rating 3.0 raised the threshold from 25; the classic `kast` field keeps the demo's own assist events only. |
| Multi-kill rating | Implemented over the exclusive `multi_kills` buckets with escalating points. |
| Round swing | Partial (v1). Kills only, from a win-probability table over alive counts and bomb state. No credit for planting or defusing, no economy or per-map side percentages in the win model, no trade denials or failed trades. |
| Exact HLTV parity | Out of scope. The formula, its weights and its win-probability model are proprietary. |

## The formula at a glance

Each of the six axes is accumulated over the match, averaged per round, and divided by a baseline that defines an average performance, so **1.0 is average on every axis**:

```
sub_rating = (axis total / match rounds) / axis baseline      (swing differs, see below)

value = 0.25·kills + 0.20·damage + 0.10·survival
      + 0.15·kast  + 0.10·multi_kill + 0.20·round_swing
```

The blend weights sum to 1 and are our choice — HLTV does not publish theirs. Like ADR and KAST elsewhere in this tool, the denominator is the match's round count, so a player who joined late is diluted on every axis rather than rated only on the rounds they played.

## Equipment tiers

Every player involved in a kill or hurt event is bucketed by the strongest weapon in their inventory at that moment. The duel is priced loadout against loadout: a rifler's knife kill is still a rifle-tier duel.

| Tier | Weapons | Strength |
| --- | --- | ---: |
| Sniper | AWP, SCAR-20, G3SG1 | 3.4 |
| Tier-1 rifle | AK-47, M4A4, M4A1-S, AUG, SG 553 | 3.1 |
| Tier-2 rifle | FAMAS, Galil AR, SSG 08 | 2.55 |
| SMG / shotgun / MG | MAC-10, MP9, MP7, MP5-SD, UMP-45, P90, PP-Bizon, Nova, XM1014, MAG-7, Sawed-Off, M249, Negev | 2.0 |
| Upgraded pistol | Desert Eagle, R8, P250, Five-SeveN, Tec-9, CZ75, Dual Berettas | 1.45 |
| Starter pistol | Glock-18, USP-S, P2000 | 1.0 |

A player carrying no firearm at all (knife, zeus) counts as a starter pistol: they fight with even less, and treating them as unknown would misread the duel as even. Only a missing inventory is unknown, and every helper treats an unknown tier as an even matchup so missing data can never skew the rating.

The strengths are calibration constants. A tier with strength `a` is expected to beat a tier with strength `b` with probability `a / (a + b)`; the spread is anchored so that a tier-1 rifle beats a starter pistol about 76% of the time, which is what reproduces HLTV's published kill values below.

## Duel pricing: kills and damage

A kill is worth more the harder the duel was:

```
winProb(killer, victim) = strength(killer) / (strength(killer) + strength(victim))
killPoints              = 1.10 × 2 × (1 − winProb)
```

1.10 for an economically even duel is the value HLTV quotes. The full matrix (killer row, victim column, points per kill):

| killer \ victim | Starter | Upgr. pistol | SMG | Rifle 2 | Rifle 1 | Sniper |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Starter pistol | 1.10 | 1.30 | 1.47 | 1.58 | 1.66 | 1.70 |
| Upgraded pistol | 0.90 | 1.10 | 1.28 | 1.40 | 1.50 | 1.54 |
| SMG/shotgun/MG | 0.73 | 0.92 | 1.10 | 1.23 | 1.34 | 1.39 |
| Tier-2 rifle | 0.62 | 0.80 | 0.97 | 1.10 | 1.21 | 1.26 |
| Tier-1 rifle | 0.54 | 0.70 | 0.86 | 0.99 | 1.10 | 1.15 |
| Sniper | 0.50 | 0.66 | 0.81 | 0.94 | 1.05 | 1.10 |

The two values HLTV has published — 1.10 for the even duel, ~0.54 for a tier-1 rifle killing a starter pistol — sit on this matrix by construction; everything else follows from the strengths. Post-round kills count, exactly as they do in `kill_stats.total`.

Damage uses the same factor without the 1.10: each hurt event's real health damage is multiplied by `2 × (1 − winProb)` and accumulated as eco-damage. Zero-damage events (overlapping shotgun pellets, fully absorbed hits) are skipped.

## Eco round weight: survival and KAST

Surviving or staying useful on a weak buy counts for more than doing it with a full loadout. Each player's tier is snapshotted when freeze time ends (so it reflects what was bought), and every survived or KAST-credited round adds this weight instead of 1:

```
ecoRoundWeight(tier) = 2 × winProb(tier-1 rifle, tier)
```

| Round played on | Weight |
| --- | ---: |
| Starter pistol | 1.51 |
| Upgraded pistol | 1.36 |
| SMG / shotgun / MG | 1.22 |
| Tier-2 rifle | 1.10 |
| Tier-1 rifle | 1.00 |
| Sniper | 0.95 |
| Unknown | 1.00 |

The direction is our assumption: survival on an eco exceeds expectation, so it is worth more. The rating's KAST credit itself is kill, survival, traded death (same rules as the classic `kast`), a demo assist event (which keeps flash assists), or at least 40 damage into an enemy who died that round. Match-end cleanup deaths settle no assists, because their victim keeps survival and damage into a survivor is no assist.

The KAST axis's pre-baseline value is published as `player_map_stats.approx_ekast_percent`:

```
Approx. eKAST (%)  = eco-weighted rating-KAST credits / match rounds × 100
eKAST sub-rating   = (Approx. eKAST (%) / 100) / 0.79
```

79.0% is a sub-rating of exactly 1.00 and 100.0% is ~1.27. Because the eco weights above make a qualifying round worth more than 1 on a weak buy, the percentage can exceed 100 and is not clamped. Like every eKAST number in this tool it is an approximation — HLTV publishes eKAST values but not its economy weights, so no exact parity is claimed — and it is not the classic `kast`, which stays the exact public definition.

## Round swing

Round swing measures how much each kill moved the round. The win-probability model is a lookup table over both sides' alive counts; the value is the probability that the T side wins the round with the bomb not planted:

| T alive \ CT alive | 0 | 1 | 2 | 3 | 4 | 5 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| **0** | – | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 |
| **1** | 1.00 | 0.45 | 0.20 | 0.08 | 0.03 | 0.01 |
| **2** | 1.00 | 0.75 | 0.48 | 0.27 | 0.13 | 0.05 |
| **3** | 1.00 | 0.90 | 0.70 | 0.48 | 0.30 | 0.16 |
| **4** | 1.00 | 0.95 | 0.85 | 0.67 | 0.48 | 0.32 |
| **5** | 1.00 | 0.97 | 0.92 | 0.80 | 0.65 | 0.48 |

The anchors are hand-set from community round-outcome statistics: even counts sit at 0.48 (maps average slightly CT-sided pre-plant), a one-player advantage is worth roughly 15–20 points, and lopsided counts saturate towards certainty without reaching it. Larger game modes clamp to the 5v5 table.

A planted bomb shifts the probability by **+0.9 in log-odds** towards the T side, which lifts an even 5v5 from 0.48 to 0.69 while keeping lopsided rounds lopsided (0.97 moves to 0.99, never past certainty). Two planted cases the shift cannot express are set directly: with no CT left to defuse, the bomb always explodes (probability 1, even when no T is alive either), and with the bomb down but every T dead the round is a defuse race the CTs usually win — 0.15 / 0.08 / 0.05 / 0.03 / 0.02 T-win probability for 1–5 CTs alive.

Each enemy kill is priced as the difference it makes: the model is evaluated from the killer's side before and after the victim is removed, the killer gains the delta and the victim loses it. Swing therefore **sums to zero across every round** — what one player earns, an opponent paid for — and diminishing returns are built in: an opening T kill at 5v5 is worth +0.17, the fifth kill of a wipe far less. Post-round kills, suicides, teamkills and world deaths move nothing, and in this first version neither does planting or defusing itself.

Because the average swing is zero by construction, this axis skips the baseline division:

```
round_swing = max(0, 1 + 2.5 × swing per round)
```

A player who single-handedly moves 4% of win probability per round rates 1.10 on this axis, roughly the spread the other axes show between an average and a strong player.

The signed pre-normalization value is published as `player_map_stats.approx_round_swing_percent`:

```
Approx. swing (%)      = summed modeled win-probability deltas / match rounds × 100
Round swing sub-rating = max(0, 1 + (Approx. swing (%) / 100) × 2.5)
```

+4.0% maps to the 1.10 sub-rating and −4.0% to 0.90. The percentage skips the floor, so an extreme negative match can display below −40% while `rating.round_swing` reads 0 — the sub-rating is not a percentage and the percentage is never clamped. It is an approximation from this same alive-count and bomb-state model, with every limitation listed here unchanged, and claims no exact HLTV Round Swing parity.

## Multi-kill points

Multi-kill rounds come straight from the exclusive `multi_kills` buckets and score escalating points, because each further kill in one round is rarer and harder than the last:

| Round | Points |
| --- | ---: |
| 2k | 1 |
| 3k | 2 |
| 4k | 4 |
| 5k | 7 |

## Baselines and calibration

The baselines that define "average" were measured, not chosen:

| Axis | Baseline (per round) |
| --- | ---: |
| Eco-adjusted kill points | 0.69 |
| Eco-adjusted damage | 70.0 |
| Eco-weighted rounds survived | 0.37 |
| Eco-weighted rating-KAST rounds | 0.79 |
| Multi-kill points | 0.22 |

The calibration set is five real matches: the three integration fixtures (Premier games on Mirage, Ancient and Inferno) plus an HLTV pro match and a FACEIT match. Over that set the rounds-weighted mean rating is 1.000 and every sub-rating mean lands within 2% of it. As a sanity spread, a 26-6 map on that set rates 1.81 and a 4-14 map 0.49.

To recalibrate — after changing tier strengths, the win-probability table, or when a larger demo set is available — run the analyser over the set, compute the rounds-weighted mean of each sub-rating, and multiply each baseline by its mean; a mean of 1.0 on every axis means calibrated. `TestBlendRatingOfBaselineAveragesIsOne` pins the contract that exact-baseline input rates exactly 1.00, and `TestKillPointsMatchHLTVAnchors` pins the published HLTV example values, so those two tests tell you whether a change needs recalibration or broke an anchor.

## Known divergences from the real Rating 3.0

- The formula, blend weights and win-probability model are HLTV's proprietary work; everything here is a reconstruction from their public description.
- Trade denials and failed trades do not feed the kill axis yet.
- The win-probability model knows nothing of per-map side percentages, remaining economy or time left in the round.
- The calibration set is small and skews towards standard 5v5 competitive play. Ratings are still computed for respawn and wingman modes but are not meaningful there.
- Extremes are compressed relative to HLTV: a map HLTV would rate around 2.1 lands near 1.8 here.
