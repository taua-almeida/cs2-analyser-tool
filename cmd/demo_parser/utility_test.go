package demoparser

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	common "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	events "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
	st "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/sendtables"
)

type flashTestProvider struct{}

func (flashTestProvider) IngameTick() int                              { return 0 }
func (flashTestProvider) TickRate() float64                            { return 64 }
func (flashTestProvider) FindPlayerByHandle(uint64) *common.Player     { return nil }
func (flashTestProvider) FindPlayerByPawnHandle(uint64) *common.Player { return nil }
func (flashTestProvider) FindWeaponByEntityID(int) *common.Equipment   { return nil }
func (flashTestProvider) FindEntityByHandle(uint64) st.Entity          { return nil }

func flashedPlayer(id uint64, name string, team common.Team, duration float32) *common.Player {
	p := common.NewPlayer(flashTestProvider{})
	p.SteamID64 = id
	p.UserID = int(id)
	p.Name = name
	p.Team = team
	p.FlashDuration = duration
	return p
}

func TestFlashStatsAttributeEventsAndDeriveAverage(t *testing.T) {
	attacker := flashedPlayer(shooterID, "attacker", common.TeamTerrorists, 0)
	enemy := flashedPlayer(victimID, "enemy", common.TeamCounterTerrorists, 1.5)
	friend := flashedPlayer(3, "friend", common.TeamTerrorists, 2)
	a := liveAnalyser(attacker, enemy, friend)

	a.onPlayerFlashed(events.PlayerFlashed{Player: enemy, Attacker: attacker})
	a.parser.(*matchParser).currentTime = 2 * time.Second
	enemy.FlashDuration = 2.5
	a.onPlayerFlashed(events.PlayerFlashed{Player: enemy, Attacker: attacker})
	a.onPlayerFlashed(events.PlayerFlashed{Player: friend, Attacker: attacker})
	attacker.FlashDuration = 4
	a.onPlayerFlashed(events.PlayerFlashed{Player: attacker, Attacker: attacker})
	a.derive(0)

	got := a.players[shooterID].UtilityStats
	if got.EnemiesFlashed != 2 {
		t.Errorf("enemies flashed = %d, want 2", got.EnemiesFlashed)
	}
	if got.FriendsFlashed != 1 {
		t.Errorf("friends flashed = %d, want 1", got.FriendsFlashed)
	}
	if !closeTo(got.EnemyFlashTimeSeconds, 4) {
		t.Errorf("enemy flash time = %v, want 4", got.EnemyFlashTimeSeconds)
	}
	if !closeTo(got.AverageEnemyFlashTimeSeconds, 2) {
		t.Errorf("average enemy flash time = %v, want 2", got.AverageEnemyFlashTimeSeconds)
	}
}

func TestRepeatedFlashCountsOnlyNewBlindTime(t *testing.T) {
	attacker := flashedPlayer(shooterID, "attacker", common.TeamTerrorists, 0)
	enemy := flashedPlayer(victimID, "enemy", common.TeamCounterTerrorists, 4)
	a := liveAnalyser(attacker, enemy)

	a.onPlayerFlashed(events.PlayerFlashed{Player: enemy, Attacker: attacker})
	a.parser.(*matchParser).currentTime = 2 * time.Second
	a.onPlayerFlashed(events.PlayerFlashed{Player: enemy, Attacker: attacker})
	a.derive(0)

	got := a.players[shooterID].UtilityStats
	if got.EnemiesFlashed != 2 {
		t.Errorf("enemies flashed = %d, want 2 events", got.EnemiesFlashed)
	}
	if !closeTo(got.EnemyFlashTimeSeconds, 6) {
		t.Errorf("enemy flash time = %v, want 6 seconds of non-overlapping blind time", got.EnemyFlashTimeSeconds)
	}
	if !closeTo(got.AverageEnemyFlashTimeSeconds, 3) {
		t.Errorf("average enemy flash time = %v, want 3", got.AverageEnemyFlashTimeSeconds)
	}
}

func TestAverageEnemyFlashTimeIsZeroWithoutEnemyFlashes(t *testing.T) {
	a := liveAnalyser()
	a.players[shooterID] = &DemoPlayer{SteamID: shooterID}

	a.derive(0)

	if got := a.players[shooterID].UtilityStats.AverageEnemyFlashTimeSeconds; got != 0 {
		t.Errorf("average enemy flash time = %v, want 0", got)
	}
}

func TestFlashStatsIgnoreWarmup(t *testing.T) {
	attacker := flashedPlayer(shooterID, "attacker", common.TeamTerrorists, 0)
	enemy := flashedPlayer(victimID, "enemy", common.TeamCounterTerrorists, 2)
	a := liveAnalyser(attacker, enemy)
	a.parser.(*matchParser).warmup = true

	a.onPlayerFlashed(events.PlayerFlashed{Player: enemy, Attacker: attacker})

	if len(a.players) != 0 {
		t.Errorf("players created during warmup = %d, want 0", len(a.players))
	}
}

func TestFlashAssistKeepsExistingJSONKey(t *testing.T) {
	data, err := json.Marshal(DemoPlayer{AssistStats: AssistStats{FlashedEnemies: 3}})
	if err != nil {
		t.Fatalf("marshalling player: %v", err)
	}
	jsonText := string(data)
	if !strings.Contains(jsonText, `"assist_stats":{"total":0,"flashed_enemies":3`) {
		t.Errorf("flash assists missing from assist_stats: %s", jsonText)
	}
	if got := strings.Count(jsonText, `"flashed_enemies"`); got != 1 {
		t.Errorf("flashed_enemies key count = %d, want 1", got)
	}
}

func utilityHurt(a *analyser, attacker, victim *common.Player, equipmentType common.EquipmentType, reported, before, after int) {
	a.lastHealth[trackerID(victim)] = before
	a.onPlayerHurt(events.PlayerHurt{
		Player:            victim,
		Attacker:          attacker,
		Weapon:            &common.Equipment{Type: equipmentType},
		HealthDamageTaken: reported,
		Health:            after,
	})
}

func TestUtilityDamageUsesRealEnemyHealthRemoved(t *testing.T) {
	attacker := player(shooterID, "attacker", common.TeamTerrorists)
	a := liveAnalyser(attacker)

	// The HE reports 80, but the victim only loses 10 real health.
	utilityHurt(a, attacker, player(10, "he", common.TeamCounterTerrorists), common.EqHE, 80, 100, 90)
	utilityHurt(a, attacker, player(11, "molotov", common.TeamCounterTerrorists), common.EqMolotov, 12, 100, 88)
	utilityHurt(a, attacker, player(12, "incendiary", common.TeamCounterTerrorists), common.EqIncendiary, 8, 100, 92)

	got := a.players[shooterID].UtilityStats.UtilityDamage
	want := UtilityDamageStats{Total: 30, HE: 10, Fire: 20}
	if got != want {
		t.Errorf("utility damage = %+v, want %+v", got, want)
	}
}

func TestUtilityDamageExcludesOtherWeaponsTeamSelfAndBombDamage(t *testing.T) {
	attacker := player(shooterID, "attacker", common.TeamTerrorists)
	enemy := player(victimID, "enemy", common.TeamCounterTerrorists)
	teammate := player(3, "teammate", common.TeamTerrorists)
	a := liveAnalyser(attacker, enemy, teammate)

	utilityHurt(a, attacker, enemy, common.EqAK47, 20, 100, 80)
	utilityHurt(a, attacker, enemy, common.EqFlash, 5, 100, 95)
	utilityHurt(a, attacker, teammate, common.EqHE, 20, 100, 80)
	utilityHurt(a, attacker, attacker, common.EqHE, 20, 100, 80)
	utilityHurt(a, attacker, enemy, common.EqBomb, 20, 100, 80)

	got := a.players[shooterID]
	if got.UtilityStats.UtilityDamage != (UtilityDamageStats{}) {
		t.Errorf("utility damage = %+v, want zero", got.UtilityStats.UtilityDamage)
	}
	if got.AssistStats.DamageGiven != 25 {
		t.Errorf("damage given = %d, want 25 from the gun and flash projectile hits", got.AssistStats.DamageGiven)
	}
}

func grenadeThrow(thrower *common.Player, equipmentType common.EquipmentType) events.GrenadeProjectileThrow {
	return events.GrenadeProjectileThrow{Projectile: &common.GrenadeProjectile{
		Thrower:        thrower,
		WeaponInstance: &common.Equipment{Type: equipmentType},
	}}
}

func TestGrenadesThrownCountsEachStandardType(t *testing.T) {
	thrower := player(shooterID, "thrower", common.TeamTerrorists)
	a := liveAnalyser(thrower)

	for _, equipmentType := range []common.EquipmentType{
		common.EqFlash,
		common.EqSmoke,
		common.EqHE,
		common.EqMolotov,
		common.EqIncendiary,
		common.EqDecoy,
		common.EqSnowball,
	} {
		a.onGrenadeProjectileThrow(grenadeThrow(thrower, equipmentType))
	}

	got := a.players[shooterID].UtilityStats.GrenadesThrown
	want := GrenadesThrownStats{Total: 6, Flash: 1, Smoke: 1, HE: 1, Molotov: 1, Incendiary: 1, Decoy: 1}
	if got != want {
		t.Errorf("grenades thrown = %+v, want %+v", got, want)
	}
}

func TestGrenadesThrownRecoversUnknownModelFromActiveInventory(t *testing.T) {
	thrower := player(shooterID, "thrower", common.TeamTerrorists)
	// A plain test player has no pawn entity, so ActiveWeaponID is zero. The
	// real parser's inventory uses the live active entity ID in the same way.
	thrower.Inventory = map[int]*common.Equipment{0: {Type: common.EqFlash}}
	a := liveAnalyser(thrower)

	a.onGrenadeProjectileThrow(grenadeThrow(thrower, common.EqUnknown))

	want := GrenadesThrownStats{Total: 1, Flash: 1}
	if got := a.players[shooterID].UtilityStats.GrenadesThrown; got != want {
		t.Errorf("grenades thrown = %+v, want %+v", got, want)
	}
}

func TestGrenadeThrowsIgnoreWarmup(t *testing.T) {
	thrower := player(shooterID, "thrower", common.TeamTerrorists)
	a := liveAnalyser(thrower)
	a.parser.(*matchParser).warmup = true

	a.onGrenadeProjectileThrow(grenadeThrow(thrower, common.EqFlash))

	if len(a.players) != 0 {
		t.Errorf("players created during warmup = %d, want 0", len(a.players))
	}
}

type reserveEntity struct {
	st.Entity
	reserve int
}

func (e reserveEntity) Property(name string) st.Property {
	if name != "m_pReserveAmmo.0000" {
		return nil
	}
	return reserveProperty{reserve: e.reserve}
}

type reserveProperty struct {
	st.Property
	reserve int
}

func (p reserveProperty) Value() st.PropertyValue {
	return st.PropertyValue{Any: int32(p.reserve)}
}

func TestStandardGrenadePrices(t *testing.T) {
	want := map[common.EquipmentType]int{
		common.EqFlash:      200,
		common.EqSmoke:      300,
		common.EqHE:         300,
		common.EqMolotov:    400,
		common.EqIncendiary: 500,
		common.EqDecoy:      50,
	}
	if len(standardGrenadePrices) != len(want) {
		t.Fatalf("price table has %d entries, want %d", len(standardGrenadePrices), len(want))
	}
	for equipmentType, price := range want {
		if got := standardGrenadePrices[equipmentType]; got != price {
			t.Errorf("%s price = %d, want %d", equipmentType, got, price)
		}
	}
}

func TestUnusedUtilityValueUsesInventoryQuantity(t *testing.T) {
	inventory := map[int]*common.Equipment{
		1: {Type: common.EqFlash, Entity: reserveEntity{reserve: 1}},
		2: {Type: common.EqSmoke},
		3: {Type: common.EqHE},
		4: {Type: common.EqMolotov},
		5: {Type: common.EqIncendiary},
		6: {Type: common.EqDecoy},
		7: {Type: common.EqAK47},
	}

	// Two flashes plus one of every other standard grenade.
	if got, want := unusedUtilityValue(inventory), 1950; got != want {
		t.Errorf("unused utility value = %d, want %d", got, want)
	}
}

func TestDeathsAndUnusedUtilityCountEveryQualifyingDeathCause(t *testing.T) {
	tests := []struct {
		name string
		kill events.Kill
	}{
		{name: "combat", kill: events.Kill{Killer: player(shooterID, "killer", common.TeamTerrorists), Weapon: &common.Equipment{Type: common.EqAK47}}},
		{name: "suicide", kill: events.Kill{Weapon: &common.Equipment{Type: common.EqAK47}}},
		{name: "fall", kill: events.Kill{Weapon: &common.Equipment{Type: common.EqWorld}}},
		{name: "bomb", kill: events.Kill{Weapon: &common.Equipment{Type: common.EqBomb}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			victim := player(victimID, "victim", common.TeamCounterTerrorists)
			victim.Inventory = map[int]*common.Equipment{1: {Type: common.EqDecoy}}
			a := liveAnalyser(victim)
			a.onRoundStart(events.RoundStart{})
			tt.kill.Victim = victim
			if tt.name == "suicide" {
				tt.kill.Killer = victim
			}

			a.onKill(tt.kill)

			got := a.players[victimID]
			if got.Deaths != 1 {
				t.Errorf("deaths = %d, want 1", got.Deaths)
			}
			if want := (SideCount{Total: 1, CT: 1}); got.SideStats.Deaths != want {
				t.Errorf("side deaths = %+v, want %+v", got.SideStats.Deaths, want)
			}
			if got.UtilityStats.UnusedUtilityValue != 50 {
				t.Errorf("unused utility value = %d, want 50", got.UtilityStats.UnusedUtilityValue)
			}
		})
	}
}

func TestUnusedUtilityCountsRepeatedAndPostRoundCombatDeaths(t *testing.T) {
	a, killer, victim := liveRound()
	victim.Inventory = map[int]*common.Equipment{1: {Type: common.EqSmoke}}
	combatDeath := events.Kill{Killer: killer, Victim: victim, Weapon: &common.Equipment{Type: common.EqAK47}}

	a.onKill(combatDeath)
	a.onKill(combatDeath)
	a.parser.(*matchParser).gamePhase = common.GamePhaseGameEnded
	a.onRoundEnd(events.RoundEnd{Winner: common.TeamTerrorists})
	a.onKill(combatDeath)
	a.onKill(events.Kill{Killer: victim, Victim: victim, Weapon: &common.Equipment{Type: common.EqWorld}})
	a.onRoundEndOfficial(events.RoundEndOfficial{})
	// CS2 can dispatch the match cleanup after RoundEndOfficial. Finalizing
	// the tracker must not erase the fact that this World death is cleanup.
	a.onKill(events.Kill{Killer: victim, Victim: victim, Weapon: &common.Equipment{Type: common.EqWorld}})

	got := a.players[victimID]
	if got.Deaths != 3 {
		t.Errorf("deaths = %d, want 3", got.Deaths)
	}
	if want := (SideCount{Total: 3, CT: 3}); got.SideStats.Deaths != want {
		t.Errorf("side deaths = %+v, want %+v", got.SideStats.Deaths, want)
	}
	if got.UtilityStats.UnusedUtilityValue != 900 {
		t.Errorf("unused utility value = %d, want 900", got.UtilityStats.UnusedUtilityValue)
	}
}

func TestMatchEndWorldCleanupDoesNotCountDeath(t *testing.T) {
	a, _, victim := liveRound()
	a.parser.(*matchParser).gamePhase = common.GamePhaseGameEnded
	a.onRoundEnd(events.RoundEnd{Winner: common.TeamTerrorists})

	a.onKill(events.Kill{Killer: victim, Victim: victim, Weapon: &common.Equipment{Type: common.EqWorld}})

	got := a.players[victimID]
	if got.Deaths != 0 {
		t.Errorf("deaths = %d, want 0", got.Deaths)
	}
	if got.SideStats.Deaths != (SideCount{}) {
		t.Errorf("side deaths = %+v, want none", got.SideStats.Deaths)
	}
}

func TestPostRoundWorldDeathCountsBeforeMatchEnd(t *testing.T) {
	a, _, victim := liveRound()
	a.parser.(*matchParser).gamePhase = common.GamePhaseStartGamePhase
	a.onRoundEnd(events.RoundEnd{Winner: common.TeamTerrorists})

	a.onKill(events.Kill{Killer: victim, Victim: victim, Weapon: &common.Equipment{Type: common.EqWorld}})

	got := a.players[victimID]
	if got.Deaths != 1 {
		t.Errorf("deaths = %d, want 1", got.Deaths)
	}
	if want := (SideCount{Total: 1, CT: 1}); got.SideStats.Deaths != want {
		t.Errorf("side deaths = %+v, want %+v", got.SideStats.Deaths, want)
	}
}
