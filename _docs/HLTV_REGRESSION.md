# HLTV regression harness

`TestHLTVRegression` compares eight independently parsed maps with static HLTV
oracles. The original Rooster–Mindfreak fixture remains three maps and 30
player-map rows. A Spirit–MOUZ BO5 and standalone Spirit–JiJieHao Mirage add
five maps and 50 rows.

The test never contacts HLTV or downloads demos. The committed JSON records
source URLs, reviewed SteamID64 mappings, expected values, demo filenames, and
SHA-256 digests. Demo bytes remain external, read-only inputs.

## Demo setup

The original commands and directory layout are unchanged:

```text
<HLTV_DEMO_DIR>/
├── rooster-vs-mindfreak-m1-inferno.dem
├── rooster-vs-mindfreak-m2-anubis.dem
└── rooster-vs-mindfreak-m3-mirage.dem
```

Additional demos may live in one directory or several. Set
`HLTV_EXTRA_DEMO_DIRS` to an OS path-list (`:` on Unix, `;` on Windows); the
harness searches each entry for every filename:

```text
spirit-vs-mouz-m1-dust2.dem
spirit-vs-mouz-m2-mirage.dem
spirit-vs-mouz-m3-ancient.dem
spirit-vs-mouz-m4-nuke.dem
spirit-vs-jijiehao-mirage.dem
```

| Fixture | Map | Map ID | SHA-256 |
| --- | --- | ---: | --- |
| Rooster–Mindfreak | Inferno | 234944 | `60129e983bb529bd77b642d59bd2e172367b6ab0dbe73849bb656f7eb76d43c4` |
| Rooster–Mindfreak | Anubis | 234945 | `7b2a1c89ea0b99be5d4874452716f25cfcbd49353c49b3402cc107f0c5a4bcae` |
| Rooster–Mindfreak | Mirage | 234947 | `53a3ab2814af90cb9898f2e8f3d7d14ae254d94f020de418ec307e57abd7008a` |
| Spirit–MOUZ | Dust2 | 234227 | `06075d8cd46422ea9cfd989a4eb849556cd87bcce6b5c83c6343d8e031c9ae19` |
| Spirit–MOUZ | Mirage | 234233 | `a1d8cc6e2f9e709d17519157fd577f98db98ea80edd2177c12c9b8c666b11c89` |
| Spirit–MOUZ | Ancient | 234238 | `4ddc87a2b87ff604ccfeb5ee498f543b45afa1f22e8366163db73e828eeb0785` |
| Spirit–MOUZ | Nuke | 234256 | `11596e71c48615b49248b9b3e915ca0b1beaaf29230af90ceb8cbde9267b4337` |
| Spirit–JiJieHao | Mirage | 234956 | `d68a4453645332f2a1da37acb07b1e4621362145e678e056f5252b213df2dd38` |

## Commands and missing-demo behavior

Normal test runs validate the JSON and skip unconfigured external maps:

```sh
go test -count=1 ./...
```

The original fixture still runs with:

```sh
HLTV_DEMO_DIR=/path/to/match-129241-demos \
REQUIRE_HLTV_DEMOS=1 \
  go test -count=1 -v ./cmd/demo_parser -run '^TestHLTVRegression$'
```

Run the complete eight-map harness with:

```sh
HLTV_DEMO_DIR=/path/to/match-129241-demos \
REQUIRE_HLTV_DEMOS=1 \
HLTV_EXTRA_DEMO_DIRS=/path/to/match-128974-demos:/path/to/map-234956-demo \
REQUIRE_HLTV_EXTRA_DEMOS=1 \
  go test -count=1 -v ./cmd/demo_parser -run '^TestHLTVRegression$'
```

`REQUIRE_HLTV_DEMOS=1` applies only to the original fixture;
`REQUIRE_HLTV_EXTRA_DEMOS=1` applies only to the five additional maps. An
unset directory variable or missing file becomes a failure only for its
required group. A checksum mismatch always fails.

Original subtests remain `inferno`, `anubis`, and `mirage`. Additional names
are unique, for example:

```text
extra/match-128974/dust2-234227
extra/match-128974/mirage-234233
extra/match-128974/ancient-234238
extra/match-128974/nuke-234256
extra/match-2396559/mirage-234956
```

## Comparison contract

Each map requires exactly the ten oracle SteamIDs. Map name, round count,
final score values, kills, and deaths are strict. Score values are sorted
because the parser exposes final CT/T fields rather than stable team identity.

ADR is compared after both values are formatted to one decimal. KAST is
compared as an integer qualifying-round count:

```text
round(displayed percentage × map rounds / 100)
```

Expected-difference keys contain fixture identity, map ID, SteamID, and metric.
They are exact exceptions, not tolerances: a third value fails, and reaching
parity fails until the stale row is removed. ADR follow-ups are independent of
issue #38.

## Authoritative scored rounds

Round-derived facts are finalized and queued until `TotalRoundsPlayed`
advances. The queue survives a later `RoundStart`, so delayed replication of
that property cannot silently lose a genuine round. When several candidates
wait for one scored-round slot, a candidate that received `RoundEnd` is used
before an unended same-score setup candidate; unresolved extras are discarded
at parse end. This gate covers participation, KAST, survival, rating-KAST,
rating kill/damage points, trades, openings, clutches, multi-kills, aces,
swing, and MVPs. Raw event totals—kills, deaths, assists, damage, and
utility—are deliberately unchanged.

The Spirit–MOUZ Dust2 demo demonstrates the bug: it starts a setup round at
score 0:0, then starts again at 0:0 without a scored result. Before the gate,
21 scored rounds produced 22 participant rounds and one extra KAST round for
every player. After the gate, every player has 21 participant rounds and all
four BO5 maps reach 40/40 classic-KAST parity. Rating changes caused by this
filter are shared-round-fact corrections, not formula changes.

## Current oracle result and hard gate

With the pinned demos:

| Group | Kills | Deaths | ADR | Classic KAST |
| --- | ---: | ---: | ---: | ---: |
| Original fixture | 30/30 | 30/30 | 30/30 | 29/30 |
| Additional fixtures | 50/50 | 50/50 | 43/50 | 49/50 |

The ADR exceptions are all exactly 0.1: six occur in the BO5 (Dust2 sh1ro and
PR, Mirage sh1ro, Ancient xelex, Nuke xertioN and PR), and one occurs in the
standalone Mirage (0SAMAS). They are retained as out-of-scope evidence rather
than widened tolerances. This is seven rows; the earlier investigation note
counted only the six BO5 rows.

Two classic-KAST rows remain:

| Fixture | Player | Round | Evidence | Aggregate difference |
| --- | --- | ---: | --- | --- |
| Original Inferno | kairo | 9 | Died to Skullhunter at tick 76714. Skullhunter killed 1angel at 76991 and died at 77068. kairo-to-revenge is 354 ticks (5.531 s). No K/A/S/T under the supported rule. | Tool 17/22, HLTV 18/22 |
| Standalone Mirage | magixx | 22 | Died to bibu at tick 176030. tN1R killed m1N1 at 176051 and bibu at 176358. magixx-to-revenge is 328 ticks (5.125 s). No K/A/S/T under the supported rule. | Tool 17/23, HLTV 18/23 |

`TestEvaluateHLTVTradeModels` replays all eight maps through 18 combinations:
one death versus multiple deaths per revenge kill, earliest versus nearest
eligible death, exact time versus timestamp-resolution and normalized-tick
boundaries, and post-round exclusion versus inclusion. The best general model
is the production rule—one oldest death, a normalized five-second tick
boundary—which reaches 29/30 original and 49/50 additional rows. Nearest and
multiple-death attribution regress already-correct rows. Post-round eligibility
does not change these eight aggregates, though it remains independently tested.

A six-second boundary fixes the two rows above but creates other aggregate
regressions. Absolute-clock rounding is rejected because its effective window
depends on the demo clock's phase. A scalar cutoff selected between observed
ticks is also rejected as fitted: the original Mirage contains an already
correct 358-tick control. No evaluated general rule reaches every oracle, so
the plan's hard gate applies: the two exact #38 exceptions remain and issue
#38 must not be closed. A focused follow-up needs either an independent
per-round HLTV oracle or additional evidence for multi-kill trade sequencing.

Run the model matrix with the same demo paths plus:

```sh
HLTV_EVALUATE_TRADE_MODELS=1 \
  go test -count=1 -v ./cmd/demo_parser -run '^TestEvaluateHLTVTradeModels$'
```

For event-level evidence on one player, set a demo and optional SteamID:

```sh
HLTV_TRACE_DEMO=/path/to/map.dem \
HLTV_TRACE_STEAM_ID=76561199063238565 \
  go test -count=1 -v ./cmd/demo_parser -run '^TestTraceHLTVRoundEvidence$'
```

The trace records participation, side, kills, normal/flash assists, death
cause/frame/time/tick, every direct trade candidate and trading kill, survival
at `RoundEnd` and `RoundEndOfficial`, disconnects, scored status, rating-assist
status, and the final classic-KAST reasons. It writes no files.

## Original issue #38 event evidence

HLTV publishes map-level KAST, not per-round flags. The table records the demo
events whose rule changes explain the original 13 aggregate differences. Each
player participated, was dead at both end events, and had no disconnect or
reconnect in the listed round. `K`, `A`, `S`, and `T` are classic-KAST facts
after flash assists are excluded.

| Map | Player | Round | Event-level evidence | Result |
| --- | --- | ---: | --- | --- |
| Inferno | chelleos | 20 | Flash assist at tick 156870; then died with no K/A/S/T. | Removed one flash-only qualification; parity. |
| Inferno | rekonz | 14, 18 | Later of two deaths to one killer before one revenge kill (110026/110128→110222 and 139499/139550→139555); T was the only reason. | Oldest-death attribution removes two; parity. |
| Inferno | JD | 15 | Second of three deaths at 119653/119692/119779 before revenge at 119809; T only. | Oldest-death attribution removes one; parity. |
| Inferno | ADK | 2 | Later death at 23737 after 23523 before revenge at 23794; T only. | Oldest-death attribution removes one; parity. |
| Inferno | void | 4, 8 | Round 4 was flash-only. In round 8, last of deaths at 70085/70155/70229 before revenge at 70330; T only. | Removes two; parity. |
| Inferno | kairo | 9 | Death at 76714; killer died at 77068, 354 ticks later. | Unresolved; HLTV has one more. |
| Inferno | phoebe | 4 | Second of deaths at 42017/42037/42062 before revenge at 42319; T only. | Oldest-death attribution removes one; parity. |
| Inferno | lawlkay | 4 | Third death in the same sequence; T only. | Oldest-death attribution removes one; parity. |
| Anubis | ADK | 11 | Death at 83051 and revenge at 83372 are 321 ticks apart. Their tick intervals can be exactly five seconds apart. | Normalized tick boundary adds T; parity. |
| Anubis | zune | 17 | Flash assist at 127990; then died with no K/A/S/T. | Removed one flash-only qualification; parity. |
| Anubis | void | 5 | Flash assist at 33824; then died with no K/A/S/T. | Removed one flash-only qualification; parity. |
| Mirage | zune | 10 | Later death at 100974 after 100855 before revenge at 100979; T only. | Oldest-death attribution removes one; parity. |
| Mirage | kairo | 13, 23 | Round 13: later death at 121603 after 121355 before revenge at 121657; T only. Round 23: flash-only assist. | Removes two; parity. |

Rating 3.0 remains approximate because HLTV's formula is proprietary. The
harness reports MAE, RMSE, bias, Spearman correlation, and counts within
±0.05/0.10/0.20; it does not assert exact rating parity.
