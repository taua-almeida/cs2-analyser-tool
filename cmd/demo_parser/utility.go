package demoparser

import (
	"time"

	common "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
)

// standardGrenadePrices holds normal Competitive purchase prices from the
// CS2 economy in August 2026. Equipment exposes inventory quantity but no
// price, so game economy changes must be reflected here explicitly.
var standardGrenadePrices = map[common.EquipmentType]int{
	common.EqFlash:      200,
	common.EqSmoke:      300,
	common.EqHE:         300,
	common.EqMolotov:    400,
	common.EqIncendiary: 500,
	common.EqDecoy:      50,
}

func isStandardGrenade(equipmentType common.EquipmentType) bool {
	_, ok := standardGrenadePrices[equipmentType]
	return ok
}

// projectileGrenadeType returns the standard grenade represented by a throw.
// demoinfocs normally resolves the projectile model; when that lookup misses,
// the grenade is still the thrower's active inventory item at dispatch time.
// Known game-mode-only equipment is returned unchanged so add can ignore it.
func projectileGrenadeType(projectile *common.GrenadeProjectile) common.EquipmentType {
	if projectile == nil {
		return common.EqUnknown
	}
	if projectile.WeaponInstance != nil && projectile.WeaponInstance.Type != common.EqUnknown {
		return projectile.WeaponInstance.Type
	}
	if projectile.Thrower == nil {
		return common.EqUnknown
	}
	active := projectile.Thrower.Inventory[projectile.Thrower.ActiveWeaponID()]
	if active != nil && isStandardGrenade(active.Type) {
		return active.Type
	}
	return common.EqUnknown
}

// unusedUtilityValue prices only the six standard purchasable grenades in an
// inventory. Magazine plus reserve represents stacked flashbangs correctly.
func unusedUtilityValue(inventory map[int]*common.Equipment) int {
	value := 0
	for _, equipment := range inventory {
		if equipment == nil {
			continue
		}
		price, ok := standardGrenadePrices[equipment.Type]
		if !ok {
			continue
		}
		quantity := equipment.AmmoInMagazine() + equipment.AmmoReserve()
		value += price * quantity
	}
	return value
}

// addedFlashTime returns only the part of the reported blind interval that
// extends beyond blind time already observed for this player. PlayerFlashed's
// duration is the player's current m_flFlashDuration, so summing it directly
// would count the overlap again when a blinded player is re-flashed.
func (a *analyser) addedFlashTime(player *common.Player, reported time.Duration) time.Duration {
	now := a.parser.CurrentTime()
	newEnd := now + max(reported, 0)
	playerID := trackerID(player)
	previousEnd := a.flashEnds[playerID]
	if newEnd <= previousEnd {
		return 0
	}
	a.flashEnds[playerID] = newEnd
	if previousEnd > now {
		return newEnd - previousEnd
	}
	return newEnd - now
}

// add credits capped enemy health damage to the utility type that caused it.
// Fire combines molotov and incendiary damage because hurt events do not
// reliably preserve which grenade created an inferno.
func (stats *UtilityDamageStats) add(equipmentType common.EquipmentType, damage int) {
	switch equipmentType {
	case common.EqHE:
		stats.HE += damage
	case common.EqMolotov, common.EqIncendiary:
		stats.Fire += damage
	default:
		return
	}
	stats.Total += damage
}

// add counts one resolved throw of a standard purchasable grenade. Warmup and
// player attribution are handled by the event handler before this method.
func (stats *GrenadesThrownStats) add(equipmentType common.EquipmentType) {
	if !isStandardGrenade(equipmentType) {
		return
	}
	stats.Total++
	switch equipmentType {
	case common.EqFlash:
		stats.Flash++
	case common.EqSmoke:
		stats.Smoke++
	case common.EqHE:
		stats.HE++
	case common.EqMolotov:
		stats.Molotov++
	case common.EqIncendiary:
		stats.Incendiary++
	case common.EqDecoy:
		stats.Decoy++
	}
}
