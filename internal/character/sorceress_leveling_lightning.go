package character

import (
	"fmt"
	"log/slog"
	"math"
	"sort"
	"time"

	"github.com/hectorgimenez/d2go/pkg/data"
	"github.com/hectorgimenez/d2go/pkg/data/difficulty"
	"github.com/hectorgimenez/d2go/pkg/data/npc"
	"github.com/hectorgimenez/d2go/pkg/data/skill"
	"github.com/hectorgimenez/d2go/pkg/data/stat"
	"github.com/hectorgimenez/d2go/pkg/data/state"
	"github.com/hectorgimenez/koolo/internal/action/step"
	"github.com/hectorgimenez/koolo/internal/context"
	"github.com/hectorgimenez/koolo/internal/game"
	"github.com/hectorgimenez/koolo/internal/pather"
)

const (
	SorceressLevelingLightningMaxAttacksLoop = 40

	lightningDangerDistance = 6
	lightningSafeDistance   = 10

	// Constants for Static Field casting
	staticFieldMaxCasts         = 10 // Max casts per monster (or until health threshold)
	staticFieldCastDistance     = 10 // Max distance for Static Field

	// Base thresholds for Static Field (will be adjusted by difficulty)
	staticFieldMinLifeThresholdNormalNightmare = 35 // Stop casting Static Field if monster life is below this percent in Normal/Nightmare
	staticFieldMinLifeThresholdHell            = 60 // Stop casting Static Field if monster life is below this percent in Hell
)

type SorceressLevelingLightning struct {
	BaseCharacter
}

func (s SorceressLevelingLightning) CheckKeyBindings() []skill.ID {
	requireKeybindings := []skill.ID{}


	missingKeybindings := []skill.ID{}

	for _, cskill := range requireKeybindings {
		if _, found := s.Data.KeyBindings.KeyBindingForSkill(cskill); !found {
			missingKeybindings = append(missingKeybindings, cskill)
		}
	}

	if len(missingKeybindings) > 0 {
		s.Logger.Debug("There are missing required key bindings.", slog.Any("Bindings", missingKeybindings))
	}

	return missingKeybindings
}

// KillMonsterSequence now conforms to the interface signature.
// Static Field logic is integrated by checking the targeted monster's NPC ID.
func (s SorceressLevelingLightning) KillMonsterSequence(
	monsterSelector func(d game.Data) (data.UnitID, bool),
	skipOnImmunities []stat.Resist,
) error {
	completedAttackLoops := 0
	previousUnitID := 0
	lastReposition := time.Now()
	staticFieldCastsDone := 0 // Track static field casts for current monster

	ctx := context.Get() // Get context to access difficulty

	// List of bosses/super uniques for which Static Field is useful
	// Countess, Summoner, Pindle, Nihlathak are generally not Static Field targets.
	bossNpcsForStaticField := map[npc.ID]struct{}{
		npc.Andariel:          {},
		npc.Duriel:            {},
		npc.Mephisto:          {},
		npc.Izual:             {},
		npc.Diablo:            {},
		npc.BaalCrab:          {},
		npc.CouncilMember:     {},
		npc.CouncilMember2:    {},
		npc.CouncilMember3:    {},
		npc.AncientBarbarian:  {}, // Ancients (These constants *should* be available in d2go)
		npc.AncientBarbarian2: {}, // Ancients
		npc.AncientBarbarian3: {}, // Ancients
	}

	// Determine the Static Field life threshold based on difficulty
	currentMinLifeThreshold := staticFieldMinLifeThresholdNormalNightmare
	if ctx.CharacterCfg.Game.Difficulty == difficulty.Hell { // Correctly access configured difficulty
		currentMinLifeThreshold = staticFieldMinLifeThresholdHell
	}

	for {
		// --- START: Safe Distance Attack Logic ---
		needsRepos, dangerousMonster := s.needsRepositioning()
		if needsRepos && time.Since(lastReposition) > time.Second*1 {
			lastReposition = time.Now()

			targetID, foundTarget := monsterSelector(*s.Data)
			var targetMonster data.Monster
			if foundTarget {
				if m, found := s.Data.Monsters.FindByID(targetID); found {
					targetMonster = m
				}
			} else {
				targetMonster = dangerousMonster
			}

			s.Logger.Info(fmt.Sprintf("Dangerous monster detected at distance %d, repositioning...",
				pather.DistanceFromPoint(s.Data.PlayerUnit.Position, dangerousMonster.Position)))

			safePos, foundSafePos := s.findSafePosition(targetMonster)
			if foundSafePos {
				s.Logger.Debug(fmt.Sprintf("Teleporting to safe position: %+v", safePos))
				step.MoveTo(safePos)
				time.Sleep(time.Millisecond * 200)
			} else {
				s.Logger.Info("Could not find safe position for repositioning, attempting to tele away from closest monster")
				currentPos := s.Data.PlayerUnit.Position
				target := dangerousMonster.Position

				vectorX := currentPos.X - target.X
				vectorY := currentPos.Y - target.Y

				length := math.Sqrt(float64(vectorX*vectorX + vectorY*vectorY))
				if length == 0 {
					length = 1
				}

				extendedX := currentPos.X + int(float64(vectorX)/length*float64(lightningSafeDistance))
				extendedY := currentPos.Y + int(float64(vectorY)/length*float64(lightningSafeDistance))

				telePos := data.Position{X: extendedX, Y: extendedY}

				step.MoveTo(telePos)
				time.Sleep(time.Millisecond * 200)
			}
			continue
		}
		// --- END: Safe Distance Attack Logic ---

		id, found := monsterSelector(*s.Data)
		if !found {
			s.Logger.Debug("KillMonsterSequence exiting: Monster not found by selector.") // ADDED LOG
			return nil
		}
		if previousUnitID != int(id) {
			completedAttackLoops = 0
			staticFieldCastsDone = 0 // Reset casts for new monster
		}

		if !s.preBattleChecks(id, skipOnImmunities) {
			s.Logger.Debug("KillMonsterSequence exiting: Pre-battle checks failed (e.g., immunity).") // ADDED LOG
			return nil
		}

		if completedAttackLoops >= SorceressLevelingLightningMaxAttacksLoop {
			s.Logger.Debug("KillMonsterSequence exiting: Max attack loops reached.") // ADDED LOG
			return nil
		}

		monster, found := s.Data.Monsters.FindByID(id)
		if !found {
			s.Logger.Info("Monster not found in KillMonsterSequence after initial selection.", slog.String("monster_id", fmt.Sprintf("%d", id)))
			s.Logger.Debug("KillMonsterSequence exiting: Monster not found by ID within loop.") // ADDED LOG
			return nil
		}

		lvl, _ := s.Data.PlayerUnit.FindStat(stat.Level, 0)

		// Define attackOption here for reuse with PrimaryAttack
		var attackOption step.AttackOption

		// Determine attackOption based on whether Blizzard is active
		if s.Data.PlayerUnit.Skills[skill.Blizzard].Level > 0 {
			attackOption = step.StationaryDistance(25, 30)
		} else {
			attackOption = step.Distance(1, 5)
		}

		// --- START: Conditional & Repeated Static Field Logic ---
		// Check if the current monster is one of the designated Static Field targets
		if _, isStaticFieldTarget := bossNpcsForStaticField[monster.Name]; isStaticFieldTarget && s.Data.PlayerUnit.Skills[skill.StaticField].Level > 0 {
			monsterLife := monster.Stats[stat.Life]
			monsterMaxLife := monster.Stats[stat.MaxLife]
			monsterLifePercent := 100 // Default to 100 to avoid division by zero if max_life is 0 (shouldn't happen for valid monsters)
			if monsterMaxLife > 0 {
				monsterLifePercent = (monsterLife * 100) / monsterMaxLife // Manual calculation
			}

			currentMonsterDistance := pather.DistanceFromPoint(s.Data.PlayerUnit.Position, monster.Position)

			// Cast Static Field if within distance, life is above threshold, and not too many casts already
			if currentMonsterDistance <= staticFieldCastDistance &&
				monsterLifePercent > currentMinLifeThreshold &&
				staticFieldCastsDone < staticFieldMaxCasts {

				s.Logger.Debug("Using Static Field (conditional & repeated)",
					slog.String("monster", string(monster.Name)),
					slog.Int("life_percent", monsterLifePercent),
					slog.Int("distance", currentMonsterDistance),
					slog.Int("casts_done", staticFieldCastsDone),
					slog.Int("threshold", currentMinLifeThreshold),
				)
				// The 1 here indicates casting the skill once per iteration
				step.SecondaryAttack(skill.StaticField, id, 1, step.Distance(1, staticFieldCastDistance))
				staticFieldCastsDone++
				time.Sleep(time.Millisecond * 200) // Small delay to allow cast animation/cooldown
				continue                           // Re-evaluate after casting Static Field
			}
		}
		// --- END: Conditional & Repeated Static Field Logic ---

		if s.Data.PlayerUnit.MPPercent() < 15 && lvl.Value < 15 {
			s.Logger.Debug("Low mana, using primary attack (left-click skill, e.g., Attack/Fire Bolt)")
			step.PrimaryAttack(id, 1, false, step.Distance(1, 3))
		} else {
			if _, found := s.Data.KeyBindings.KeyBindingForSkill(skill.Blizzard); found {
				if s.Data.PlayerUnit.States.HasState(state.Cooldown) {
					s.Logger.Debug("Blizzard on cooldown, using Glacial Spike (Secondary Attack)")
					step.SecondaryAttack(skill.GlacialSpike, id, 2, attackOption)
				} else {
					s.Logger.Debug("Using Blizzard")
					step.SecondaryAttack(skill.Blizzard, id, 1, attackOption)
				}
			} else if _, found := s.Data.KeyBindings.KeyBindingForSkill(skill.Nova); found {
				s.Logger.Debug("Using Nova")
				step.SecondaryAttack(skill.Nova, id, 4, attackOption)
			} else if _, found := s.Data.KeyBindings.KeyBindingForSkill(skill.ChargedBolt); found {
				s.Logger.Debug("Using ChargedBolt")
				step.SecondaryAttack(skill.ChargedBolt, id, 4, attackOption)
			} else if _, found := s.Data.KeyBindings.KeyBindingForSkill(skill.FireBolt); found {
				s.Logger.Debug("Using FireBolt")
				step.SecondaryAttack(skill.FireBolt, id, 4, attackOption)
			} else {
				s.Logger.Debug("No secondary skills available, using primary attack")
				step.PrimaryAttack(id, 1, false, step.Distance(1, 3))
			}
		}

		completedAttackLoops++
		previousUnitID = int(id)
	}
}

// needsRepositioning checks if any dangerous monsters are too close
func (s SorceressLevelingLightning) needsRepositioning() (bool, data.Monster) {
	for _, monster := range s.Data.Monsters.Enemies() {
		if monster.Stats[stat.Life] <= 0 {
			continue
		}

		distance := pather.DistanceFromPoint(s.Data.PlayerUnit.Position, monster.Position)
		if distance < lightningDangerDistance {
			return true, monster
		}
	}

	return false, data.Monster{}
}

// findSafePosition attempts to find a safe position to teleport to.
func (s SorceressLevelingLightning) findSafePosition(targetMonster data.Monster) (data.Position, bool) {
	ctx := context.Get()
	playerPos := s.Data.PlayerUnit.Position

	const minSafeMonsterDistance = 2 // Stricter minimum safe distance from monsters

	candidatePositions := []data.Position{}

	// Try positions in the opposite direction from the dangerous monster first
	if targetMonster.UnitID != 0 {
		vectorX := playerPos.X - targetMonster.Position.X
		vectorY := playerPos.Y - targetMonster.Position.Y

		length := math.Sqrt(float64(vectorX*vectorX + vectorY*vectorY))
		if length > 0 {
			normalizedX := int(float64(vectorX) / length * float64(lightningSafeDistance))
			normalizedY := int(float64(vectorY) / length * float64(lightningSafeDistance))

			for offsetX := -3; offsetX <= 3; offsetX++ {
				for offsetY := -3; offsetY <= 3; offsetY++ {
					candidatePos := data.Position{
						X: playerPos.X + normalizedX + offsetX,
						Y: playerPos.Y + normalizedY + offsetY,
					}

					if s.Data.AreaData.IsWalkable(candidatePos) {
						candidatePositions = append(candidatePositions, candidatePos)
					}
				}
			}
		}
	}

	for angle := 0; angle < 360; angle += 10 {
		radians := float64(angle) * math.Pi / 180

		for distFromPlayer := 5; distFromPlayer <= lightningSafeDistance+5; distFromPlayer += 2 {
			dx := int(math.Cos(radians) * float64(distFromPlayer))
			dy := int(math.Sin(radians) * float64(distFromPlayer))

			basePos := data.Position{
				X: playerPos.X + dx,
				Y: playerPos.Y + dy,
			}

			for offsetX := -1; offsetX <= 1; offsetX++ {
				for offsetY := -1; offsetY <= 1; offsetY++ {
					candidatePos := data.Position{
						X: basePos.X + offsetX,
						Y: basePos.Y + offsetY,
					}

					if s.Data.AreaData.IsWalkable(candidatePos) {
						candidatePositions = append(candidatePositions, candidatePos)
					}
				}
			}
		}
	}

	if len(candidatePositions) == 0 {
		return data.Position{}, false
	}

	type scoredPosition struct {
		pos   data.Position
		score float64
	}

	scoredPositions := []scoredPosition{}

	for _, pos := range candidatePositions {
		if targetMonster.UnitID != 0 && !ctx.PathFinder.LineOfSight(pos, targetMonster.Position) {
			continue
		}

		minMonsterDist := lightningMinMonsterDistance(pos, s.Data.Monsters)

		if minMonsterDist < minSafeMonsterDistance {
			continue
		}

		targetDistance := 0
		if targetMonster.UnitID != 0 {
			targetDistance = pather.DistanceFromPoint(pos, targetMonster.Position)
		}

		// Use the original attack distances from your leveling_lightning.go
		minAttackRange := 1 // For Charged Bolt/Nova/Fire Bolt
		maxAttackRange := 5 // For Charged Bolt/Nova/Fire Bolt

		if s.Data.PlayerUnit.Skills[skill.Blizzard].Level > 0 {
			minAttackRange = 25 // For Blizzard
			maxAttackRange = 30 // For Blizzard
		}

		attackRangeScore := 0.0
		if targetDistance >= minAttackRange && targetDistance <= maxAttackRange {
			attackRangeScore = 10.0
		} else {
			if targetMonster.UnitID != 0 {
				attackRangeScore = -math.Abs(float64(targetDistance)-float64(minAttackRange+maxAttackRange)/2.0)
			}
		}

		distanceFromPlayer := pather.DistanceFromPoint(pos, playerPos)
		score := minMonsterDist*5.0 + attackRangeScore*3.0 - float64(distanceFromPlayer)*0.8

		if minMonsterDist > float64(lightningDangerDistance+2) {
			score += 5.0
		}

		scoredPositions = append(scoredPositions, scoredPosition{
			pos:   pos,
			score: score,
		})
	}

	sort.Slice(scoredPositions, func(i, j int) bool {
		return scoredPositions[i].score > scoredPositions[j].score
	})

	if len(scoredPositions) > 0 {
		s.Logger.Info(fmt.Sprintf("Found safe position with score %.2f at distance %.2f from nearest monster, target distance %d",
			scoredPositions[0].score, lightningMinMonsterDistance(scoredPositions[0].pos, s.Data.Monsters),
			pather.DistanceFromPoint(scoredPositions[0].pos, targetMonster.Position)))
		return scoredPositions[0].pos, true
	}

	return data.Position{}, false
}

// Helper function to calculate minimum monster distance.
func lightningMinMonsterDistance(pos data.Position, monsters data.Monsters) float64 {
	minDistance := math.MaxFloat64
	for _, monster := range monsters.Enemies() {
		if monster.Stats[stat.Life] <= 0 {
			continue
		}

		distance := pather.DistanceFromPoint(pos, monster.Position)
		if float64(distance) < minDistance {
			minDistance = float64(distance)
		}
	}
	return minDistance
}

// killMonster helper function - its signature is also reverted
func (s SorceressLevelingLightning) killMonster(npcID npc.ID, t data.MonsterType) error {
	s.Logger.Info(fmt.Sprintf("Starting kill sequence for %s (Type %s)", npcID, t))
	return s.KillMonsterSequence(func(d game.Data) (data.UnitID, bool) {
		m, found := d.Monsters.FindOne(npcID, t)
		if !found {
			s.Logger.Info(fmt.Sprintf("MONSTER SELECTOR: %s (Type %s) NOT FOUND. Exiting KillMonsterSequence.", npcID, t))
			return 0, false
		}
		return m.UnitID, true
	}, nil) // Removed the staticFieldTargets parameter
}

func (s SorceressLevelingLightning) BuffSkills() []skill.ID {
	skillsList := make([]skill.ID, 0)
	if _, found := s.Data.KeyBindings.KeyBindingForSkill(skill.EnergyShield); found {
		skillsList = append(skillsList, skill.EnergyShield)
	}

	if _, found := s.Data.KeyBindings.KeyBindingForSkill(skill.ThunderStorm); found {
		skillsList = append(skillsList, skill.ThunderStorm)
	}

	armors := []skill.ID{skill.ChillingArmor, skill.ShiverArmor, skill.FrozenArmor}
	for _, armor := range armors {
		if _, found := s.Data.KeyBindings.KeyBindingForSkill(armor); found {
			skillsList = append(skillsList, armor)
			break
		}
	}

	return skillsList
}

func (s SorceressLevelingLightning) PreCTABuffSkills() []skill.ID {
	return []skill.ID{}
}

// staticFieldCasts is no longer directly used for determining casts, but can be used for logging if needed.
// It's recommended to remove this function if it serves no other purpose to avoid confusion.
func (s SorceressLevelingLightning) staticFieldCasts() int {
	casts := 6 // Default for Hell, or if not explicitly set
	ctx := context.Get()

	switch ctx.CharacterCfg.Game.Difficulty {
	case difficulty.Normal:
		casts = 10
	case difficulty.Nightmare:
		casts = 10
	}
	s.Logger.Debug("Static Field casts (deprecated for new logic, consider removing if no longer relevant)", "count", casts)
	return casts
}

func (s SorceressLevelingLightning) ShouldResetSkills() bool {
	lvl, _ := s.Data.PlayerUnit.FindStat(stat.Level, 0)
	if lvl.Value >= 25 && (s.Data.PlayerUnit.Skills[skill.ChargedBolt].Level > 5 || s.Data.PlayerUnit.Skills[skill.Lightning].Level > 5) {
		s.Logger.Info("Resetting skills: Level 25+ and Nova/Lightning level > 5")
		return true
	}

	return false
}

func (s SorceressLevelingLightning) SkillsToBind() (skill.ID, []skill.ID) {
	level, _ := s.Data.PlayerUnit.FindStat(stat.Level, 0)

	skillBindings := []skill.ID{
		skill.TomeOfTownPortal,
	}
	if level.Value >= 4 {
		skillBindings = append(skillBindings, skill.FrozenArmor)
	}
	if level.Value >= 6 {
		skillBindings = append(skillBindings, skill.StaticField)
	}
	if level.Value >= 18 {
		skillBindings = append(skillBindings, skill.Teleport)
	}

	if s.Data.PlayerUnit.Skills[skill.GlacialSpike].Level > 0 {
		skillBindings = append(skillBindings, skill.GlacialSpike)
	}

	if level.Value >= 24 {
		skillBindings = append(skillBindings, skill.Blizzard)
	}

	mainSkill := skill.AttackSkill

	if s.Data.PlayerUnit.Skills[skill.Blizzard].Level >= 1 {
		skillBindings = append(skillBindings, skill.Blizzard)
	} else if s.Data.PlayerUnit.Skills[skill.Nova].Level >= 1 {
		skillBindings = append(skillBindings, skill.Nova)
	} else if s.Data.PlayerUnit.Skills[skill.ChargedBolt].Level >= 0 {
		skillBindings = append(skillBindings, skill.ChargedBolt)
	} else if s.Data.PlayerUnit.Skills[skill.FireBolt].Level >= 0 {
		skillBindings = append(skillBindings, skill.FireBolt)
	}

	if s.Data.PlayerUnit.Skills[skill.GlacialSpike].Level > 0 {
		mainSkill = skill.GlacialSpike
	} else {
		if s.Data.PlayerUnit.Skills[skill.ChargedBolt].Level > 0 {
			mainSkill = skill.ChargedBolt
		} else if s.Data.PlayerUnit.Skills[skill.FireBolt].Level > 0 {
			mainSkill = skill.FireBolt
		} else {
			mainSkill = skill.AttackSkill
		}
	}

	s.Logger.Info("Skills bound", "mainSkill", mainSkill, "skillBindings", skillBindings)
	return mainSkill, skillBindings
}

func (s SorceressLevelingLightning) StatPoints() []context.StatAllocation {
	targets := []context.StatAllocation{
		{Stat: stat.Vitality, Points: 50},
		{Stat: stat.Strength, Points: 25},
		{Stat: stat.Vitality, Points: 65},
		{Stat: stat.Strength, Points: 80},
		{Stat: stat.Vitality, Points: 95},
		{Stat: stat.Strength, Points: 45},
		{Stat: stat.Vitality, Points: 999},
	}
	return targets
}

func (s SorceressLevelingLightning) SkillPoints() []skill.ID {
	lvl, _ := s.Data.PlayerUnit.FindStat(stat.Level, 0)
	var skillPoints []skill.ID

	if lvl.Value < 25 {
		skillPoints = []skill.ID{
			skill.ChargedBolt,
			skill.ChargedBolt,
			skill.ChargedBolt,
			skill.FrozenArmor,
			skill.ChargedBolt,
			skill.StaticField,
			skill.StaticField,
			skill.ChargedBolt,
			skill.ChargedBolt,
			skill.Telekinesis,
			skill.Warmth,
			skill.ChargedBolt,
			skill.ChargedBolt,
			skill.ChargedBolt,
			skill.ChargedBolt,
			skill.ChargedBolt,
			skill.ChargedBolt,
			skill.Teleport,
			skill.ChargedBolt,
			skill.ChargedBolt,
			skill.ChargedBolt,
			skill.ChargedBolt,
			skill.ChargedBolt,
			skill.ChargedBolt,
			skill.ChargedBolt,
			skill.ChargedBolt,
			skill.Nova,
			skill.Nova,
			skill.Nova,
			skill.Nova,
			skill.Nova,
			skill.Nova,
		}
	} else {
		skillPoints = []skill.ID{
			skill.StaticField,
			skill.Telekinesis,
			skill.Teleport,
			skill.StaticField,
			skill.FrozenArmor,
			skill.IceBolt,
			skill.IceBlast,
			skill.FrostNova,
			skill.GlacialSpike,
			skill.Blizzard,
			skill.Blizzard,
			skill.Warmth,
			skill.Blizzard,
			skill.Blizzard,
			skill.Blizzard,
			skill.Blizzard,
			skill.ColdMastery,
			skill.Blizzard,
			skill.Blizzard,
			skill.Blizzard,
			skill.Blizzard,
			skill.Blizzard,
			skill.Blizzard,
			skill.Blizzard,
			skill.Blizzard,
			skill.Blizzard,
			skill.Blizzard,
			skill.Blizzard,
			skill.Blizzard,
			skill.Blizzard,
			skill.GlacialSpike,
			skill.GlacialSpike,
			skill.GlacialSpike,
			skill.GlacialSpike,
			skill.GlacialSpike,
			skill.GlacialSpike,
			skill.GlacialSpike,
			skill.GlacialSpike,
			skill.GlacialSpike,
			skill.GlacialSpike,
			skill.GlacialSpike,
			skill.GlacialSpike,
			skill.GlacialSpike,
			skill.GlacialSpike,
			skill.GlacialSpike,
			skill.GlacialSpike,
			skill.GlacialSpike,
			skill.GlacialSpike,
			skill.IceBlast,
			skill.IceBlast,
			skill.IceBlast,
			skill.IceBlast,
			skill.IceBlast,
			skill.IceBlast,
			skill.IceBlast,
			skill.IceBlast,
			skill.IceBlast,
			skill.IceBlast,
			skill.IceBlast,
			skill.IceBlast,
			skill.IceBlast,
			skill.IceBlast,
			skill.IceBlast,
			skill.IceBlast,
			skill.IceBlast,
			skill.IceBlast,
			skill.IceBlast,
			skill.IceBolt,
			skill.IceBolt,
			skill.IceBolt,
			skill.IceBolt,
			skill.IceBolt,
			skill.IceBolt,
			skill.IceBolt,
			skill.IceBolt,
			skill.IceBolt,
			skill.IceBolt,
			skill.IceBolt,
			skill.IceBolt,
			skill.IceBolt,
			skill.IceBolt,
			skill.IceBolt,
			skill.IceBolt,
			skill.IceBolt,
			skill.IceBolt,
			skill.IceBolt,
			skill.ColdMastery,
			skill.ColdMastery,
			skill.ColdMastery,
			skill.ColdMastery,
			skill.ColdMastery,
			skill.ColdMastery,
			skill.ColdMastery,
			skill.ColdMastery,
			skill.ColdMastery,
			skill.ColdMastery,
			skill.ColdMastery,
		}
	}

	s.Logger.Info("Assigning skill points", "level", lvl.Value, "skillPoints", skillPoints)
	return skillPoints
}

func (s SorceressLevelingLightning) KillCountess() error {
	return s.killMonster(npc.DarkStalker, data.MonsterTypeSuperUnique)
}

func (s SorceressLevelingLightning) KillAndariel() error {
	return s.killMonster(npc.Andariel, data.MonsterTypeNone)
}

func (s SorceressLevelingLightning) KillSummoner() error {
	return s.killMonster(npc.Summoner, data.MonsterTypeNone)
}

func (s SorceressLevelingLightning) KillDuriel() error {
	s.Logger.Info("Entering Duriel's Chamber. Temporarily disabling back-to-town checks.")

	// Store original config
	originalBackToTownCfg := s.CharacterCfg.BackToTown

	// Temporarily disable all relevant back-to-town checks
	s.CharacterCfg.BackToTown.NoHpPotions = false
	s.CharacterCfg.BackToTown.NoMpPotions = false
	s.CharacterCfg.BackToTown.EquipmentBroken = false
	s.CharacterCfg.BackToTown.MercDied = false // This will prevent going to town if merc dies

	// Use defer to ensure original config is restored when the function exits
	defer func() {
		s.CharacterCfg.BackToTown = originalBackToTownCfg
		s.Logger.Info("Restored original back-to-town checks after Duriel fight.")
	}()

	return s.killMonster(npc.Duriel, data.MonsterTypeUnique)
}

func (s SorceressLevelingLightning) KillCouncil() error {
	councilNpcs := []npc.ID{npc.CouncilMember, npc.CouncilMember2, npc.CouncilMember3}
	return s.KillMonsterSequence(func(d game.Data) (data.UnitID, bool) {
		var councilMembers []data.Monster
		for _, m := range d.Monsters {
			for _, councilNpc := range councilNpcs {
				if m.Name == councilNpc {
					councilMembers = append(councilMembers, m)
					break
				}
			}
		}

		sort.Slice(councilMembers, func(i, j int) bool {
			distanceI := s.PathFinder.DistanceFromMe(councilMembers[i].Position)
			distanceJ := s.PathFinder.DistanceFromMe(councilMembers[j].Position)
			return distanceI < distanceJ
		})

		for _, m := range councilMembers {
			return m.UnitID, true
		}

		return 0, false
	}, nil)
}

func (s SorceressLevelingLightning) KillMephisto() error {
	s.Logger.Info("Entering Mephisto's Chamber. Temporarily disabling back-to-town checks.")

	// Store original config
	originalBackToTownCfg := s.CharacterCfg.BackToTown

	// Temporarily disable all relevant back-to-town checks
	s.CharacterCfg.BackToTown.NoHpPotions = false
	s.CharacterCfg.BackToTown.NoMpPotions = false
	s.CharacterCfg.BackToTown.EquipmentBroken = false
	s.CharacterCfg.BackToTown.MercDied = false // This will prevent going to town if merc dies

	// Use defer to ensure original config is restored when the function exits
	defer func() {
		s.CharacterCfg.BackToTown = originalBackToTownCfg
		s.Logger.Info("Restored original back-to-town checks after Mephisto fight.")
	}()

	return s.killMonster(npc.Mephisto, data.MonsterTypeNone)
}

func (s SorceressLevelingLightning) KillIzual() error {
	return s.killMonster(npc.Izual, data.MonsterTypeUnique)
}

func (s SorceressLevelingLightning) KillDiablo() error {
	s.Logger.Info("Entering Diablo's Chamber. Temporarily disabling back-to-town checks.")

	// Store original config
	originalBackToTownCfg := s.CharacterCfg.BackToTown

	// Temporarily disable all relevant back-to-town checks
	s.CharacterCfg.BackToTown.NoHpPotions = false
	s.CharacterCfg.BackToTown.NoMpPotions = false
	s.CharacterCfg.BackToTown.EquipmentBroken = false
	s.CharacterCfg.BackToTown.MercDied = false // This will prevent going to town if merc dies

	// Use defer to ensure original config is restored when the function exits
	defer func() {
		s.CharacterCfg.BackToTown = originalBackToTownCfg
		s.Logger.Info("Restored original back-to-town checks after Diablo fight.")
	}()

	timeout := time.Second * 20
	startTime := time.Now()
	diabloFound := false
	const confirmationDelay = time.Second // Add confirmation delay here too
	lastSeenTime := time.Now()

	for {
		if time.Since(startTime) > timeout && !diabloFound {
			s.Logger.Error("Diablo was not found, timeout reached (initial search).")
            s.Logger.Debug("KillDiablo() exiting: Initial search timeout.") // ADDED LOG
			return nil
		}

		diablo, found := s.Data.Monsters.FindOne(npc.Diablo, data.MonsterTypeUnique)
		if !found || diablo.Stats[stat.Life] <= 0 {
			if diabloFound {
				// We were fighting Diablo, but now he's not found or dead
				if time.Since(lastSeenTime) > confirmationDelay {
					s.Logger.Info("Diablo is no longer found or is dead after confirmation delay. Kill successful!")
                    s.Logger.Debug("KillDiablo() exiting: Confirmed dead after confirmation delay.") // ADDED LOG
					return nil // Confirmed dead or permanently gone
				} else {
					s.Logger.Debug(fmt.Sprintf("Diablo momentarily not found or dead, waiting for confirmation (%.1f / %.1f sec)",
						time.Since(lastSeenTime).Seconds(), confirmationDelay.Seconds()))
					time.Sleep(100 * time.Millisecond) // Small pause while confirming
					continue // Keep looping to re-check
				}
			} else { // Diablo not found, and we haven't engaged him yet (waiting for spawn)
				time.Sleep(200 * time.Millisecond)
				continue
			}
		}

		// If we reach here, Diablo is found AND alive.
		if !diabloFound { // Only log this once when first found
            s.Logger.Info("Diablo detected, attacking")
        }
		diabloFound = true
		lastSeenTime = time.Now() // Reset last seen time when Diablo is found

		err := s.KillMonsterSequence(func(d game.Data) (data.UnitID, bool) {
			m, found := d.Monsters.FindOne(npc.Diablo, data.MonsterTypeUnique)
			if !found {
				return 0, false
			}
			return m.UnitID, true
		}, nil)

		if err != nil {
            s.Logger.Error(fmt.Sprintf("Error during KillMonsterSequence for Diablo: %v. Re-verifying Diablo's status...", err))
        }
        // After KillMonsterSequence returns, the outer loop re-checks Diablo's status.
        s.Logger.Debug("KillMonsterSequence for Diablo completed. Outer loop re-verifying Diablo's status.")
	}
}

func (s SorceressLevelingLightning) KillPindle() error {
	return s.killMonster(npc.DefiledWarrior, data.MonsterTypeSuperUnique)
}

func (s SorceressLevelingLightning) KillNihlathak() error {
	return s.killMonster(npc.Nihlathak, data.MonsterTypeSuperUnique)
}

func (s SorceressLevelingLightning) KillAncients() error {
	ancientNpcs := []npc.ID{npc.AncientBarbarian, npc.AncientBarbarian2, npc.AncientBarbarian3}
	// Loop through the ancients as they appear.
	for _, m := range s.Data.Monsters.Enemies(data.MonsterEliteFilter()) {
		for _, ancientNpc := range ancientNpcs {
			if m.Name == ancientNpc { // Check if the monster is one of the ancients
				err := s.KillMonsterSequence(func(d game.Data) (data.UnitID, bool) {
					ancient, found := d.Monsters.FindOne(m.Name, data.MonsterTypeSuperUnique)
					if !found {
						return 0, false
					}
					return ancient.UnitID, true
				}, nil)
				if err != nil {
					return err
				}
				break // Move to the next ancient after killing the current one
			}
			// Add a short sleep to avoid a tight loop if no ancients are found immediately.
			time.Sleep(100 * time.Millisecond)
		}
	}
	step.MoveTo(data.Position{X: 10062, Y: 12639}) // This move might still be relevant for positioning after ancients
	return nil
}

func (s SorceressLevelingLightning) KillBaal() error {
	s.Logger.Info("Entering Baal's Chamber. Temporarily disabling back-to-town checks.")

	// --- START: Temporarily disable back-to-town checks for this boss fight ---
	// Store original config in a new variable to avoid modifying the current character's config directly
	// and to ensure a clean restoration.
	originalBackToTownCfg := s.CharacterCfg.BackToTown

	// Temporarily disable all relevant back-to-town checks
	s.CharacterCfg.BackToTown.NoHpPotions = false
	s.CharacterCfg.BackToTown.NoMpPotions = false
	s.CharacterCfg.BackToTown.EquipmentBroken = false
	s.CharacterCfg.BackToTown.MercDied = false // This will prevent going to town if merc dies

	s.Logger.Info("Temporarily disabled back-to-town checks for Baal fight.")

	// Use defer to ensure original config is restored when the function exits (whether via return nil or error)
	defer func() {
		s.CharacterCfg.BackToTown = originalBackToTownCfg // Restore the entire struct
		s.Logger.Info("Restored original back-to-town checks after Baal fight.")
	}()
	// --- END: Temporarily disable back-to-town checks ---

	timeout := time.Second * 30 // Give it a generous timeout
	startTime := time.Now()
	baalFoundAndEngaged := false // Flag to track if Baal was ever truly found and attacked
	const confirmationDelay = time.Second // How long to wait to confirm monster is truly gone
    lastSeenTime := time.Now() // Time when Baal was last seen alive and targetable

	for { // **The robust outer loop for Baal**
		// Check for overall timeout (if Baal never spawns or never dies)
		if time.Since(startTime) > timeout && !baalFoundAndEngaged {
			s.Logger.Error("Baal was not found or killed within timeout in his chamber (initial search).")
            s.Logger.Debug("KillBaal() exiting: Initial search timeout.") // ADDED LOG
			return nil // Return nil, signaling a "completion" to the bot's higher-level state machine.
		}

		// --- Main Baal Detection Logic ---
		baal, found := s.Data.Monsters.FindOne(npc.BaalCrab, data.MonsterTypeUnique)

		if !found || baal.Stats[stat.Life] <= 0 { // Baal is either not found OR he is dead
			if baalFoundAndEngaged { // We were fighting Baal, but now he's not found or dead
				// Check if he's *truly* gone for a short duration
				if time.Since(lastSeenTime) > confirmationDelay {
					s.Logger.Info("Baal is no longer found or is dead after confirmation delay. Kill successful!")
                    s.Logger.Debug("KillBaal() exiting: Confirmed dead after confirmation delay.") // ADDED LOG
					return nil // Confirmed dead or permanently gone
				} else {
					s.Logger.Debug(fmt.Sprintf("Baal momentarily not found or dead, waiting for confirmation (%.1f / %.1f sec remaining)",
						(confirmationDelay - time.Since(lastSeenTime)).Seconds(), confirmationDelay.Seconds()))
					time.Sleep(100 * time.Millisecond) // Small pause while confirming
					continue // Keep looping to re-check
				}
			} else { // Baal not found, and we haven't engaged him yet (waiting for spawn)
				s.Logger.Debug("Baal not found or not alive yet in chamber. Waiting for spawn/data update...")
				time.Sleep(200 * time.Millisecond) // Small pause to allow game data to update
				continue // Keep looping until found or timeout
			}
		}

		// If we reach here, Baal is found AND alive.
		if !baalFoundAndEngaged { // Only log this once when first found
            s.Logger.Info("Baal detected and alive in chamber. Engaging with KillMonsterSequence.")
        }
		baalFoundAndEngaged = true
		lastSeenTime = time.Now() // Reset last seen time when Baal is found

		// Call KillMonsterSequence to perform the actual attacks.
		// Note: We don't return its error immediately. We loop back to re-check Baal's state.
		err := s.KillMonsterSequence(func(d game.Data) (data.UnitID, bool) {
			innerBaal, innerFound := d.Monsters.FindOne(npc.BaalCrab, data.MonsterTypeUnique)
			if !innerFound {
				s.Logger.Debug("KillMonsterSequence's internal selector: Baal no longer found. Signaling attack sequence to stop.")
				return 0, false // Signal to KillMonsterSequence to stop its attacks
			}
			return innerBaal.UnitID, true
		}, nil)

		if err != nil {
			s.Logger.Error(fmt.Sprintf("Error during KillMonsterSequence for Baal: %v. Re-verifying Baal's status...", err))
			// Do not return here; the outer loop will re-check Baal's status.
		}
		// If KillMonsterSequence completes (returns nil), it means its internal selector thought Baal was gone.
		// The outer 'for {}' loop will now re-run the primary detection to confirm if Baal's status.
		s.Logger.Debug("KillMonsterSequence for Baal completed. Outer loop re-verifying Baal's status.")
	}
}