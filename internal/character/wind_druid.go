package character

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/hectorgimenez/d2go/pkg/data/mode"
	"github.com/hectorgimenez/koolo/internal/context"
	"github.com/hectorgimenez/koolo/internal/game"

	"github.com/hectorgimenez/d2go/pkg/data"
	"github.com/hectorgimenez/d2go/pkg/data/npc"
	"github.com/hectorgimenez/d2go/pkg/data/skill"
	"github.com/hectorgimenez/d2go/pkg/data/stat"
	"github.com/hectorgimenez/d2go/pkg/data/state"
	"github.com/hectorgimenez/koolo/internal/action/step"
	"github.com/hectorgimenez/koolo/internal/utils"
)

const (
	druMaxAttacksLoop   = 20              // Max number of attack loops before stopping
	druMinDistance      = 2               // Min distance to maintain from target
	druMaxDistance      = 8               // Max distance to maintain from target
	druidCastingTimeout = 3 * time.Second // Timeout for casting actions
)

type WindDruid struct {
	BaseCharacter           // Inherits common character functionality
	lastCastTime  time.Time // Tracks the last time a skill was cast
}

// Verify that required skills are bound to keys
func (s WindDruid) CheckKeyBindings() []skill.ID {
	requireKeybindings := []skill.ID{skill.Hurricane, skill.OakSage, skill.CycloneArmor, skill.TomeOfTownPortal, skill.Tornado}
	missingKeybindings := make([]skill.ID, 0)

	for _, cskill := range requireKeybindings {
		if _, found := s.Data.KeyBindings.KeyBindingForSkill(cskill); !found {
			missingKeybindings = append(missingKeybindings, cskill)
		}
	}

	if len(missingKeybindings) > 0 {
		s.Logger.Debug("There are missing required key bindings.", slog.Any("Bindings", missingKeybindings))
	}

	return missingKeybindings // Returns list of unbound required skills
}

// Ensure casting animation finishes before proceeding
func (s WindDruid) waitForCastComplete() bool {
	ctx := context.Get()
	startTime := time.Now()

	for time.Since(startTime) < castingTimeout {
		ctx.RefreshGameData()

		// Check if we're no longer casting and enough time has passed since last cast
		if ctx.Data.PlayerUnit.Mode != mode.CastingSkill &&
			time.Since(s.lastCastTime) > 150*time.Millisecond {
			return true
		}

		time.Sleep(16 * time.Millisecond) // Small delay to avoid busy-waiting
	}

	return false // Returns false if timeout is reached
}

// Handle the main combat loop for attacking monsters
func (s WindDruid) KillMonsterSequence(
	monsterSelector func(d game.Data) (data.UnitID, bool), // Function to select target monster
	skipOnImmunities []stat.Resist, // Resistances to skip if monster is immune
) error {
	ctx := context.Get()
	completedAttackLoops := 0
	previousUnitID := 0
	lastBuffCheck := time.Now()

	attackOpts := []step.AttackOption{
		step.StationaryDistance(druMinDistance, druMaxDistance), // Maintains distance range
	}

	// Ensure we always return to Tornado when done
	defer func() {
		if kb, found := ctx.Data.KeyBindings.KeyBindingForSkill(skill.Tornado); found {
			ctx.HID.PressKeyBinding(kb)
		}
	}()

	for {
		// Refresh game data every 100ms like Sorceress implementation
		if time.Since(lastBuffCheck) > 100*time.Millisecond {
			ctx.RefreshGameData()
			lastBuffCheck = time.Now()
		}

		ctx.PauseIfNotPriority() // Pause if not the priority task

		id, found := monsterSelector(*s.Data)
		if !found {
			return nil
		}
		if previousUnitID != int(id) {
			completedAttackLoops = 0
		}

		if completedAttackLoops >= druMaxAttacksLoop {
			return nil // Exit if max attack loops reached
		}

		monster, found := s.Data.Monsters.FindByID(id)
		if !found {
			s.Logger.Info("Monster not found", slog.String("monster", fmt.Sprintf("%v", monster)))
			return nil
		}

		s.RecastBuffs() // Refresh buffs before attacking

		if !s.preBattleChecks(id, skipOnImmunities) { // Perform pre-combat checks
			return nil
		}

		// Tornado attack sequence
		if kb, found := ctx.Data.KeyBindings.KeyBindingForSkill(skill.Tornado); found {
			ctx.HID.PressKeyBinding(kb) // Set Tornado as active skill
			if err := step.PrimaryAttack(id, 1, true, attackOpts...); err == nil {
				if !s.waitForCastComplete() { // Wait for cast to complete
					continue
				}
				s.lastCastTime = time.Now() // Update last cast time
				completedAttackLoops++
			}
		}

		previousUnitID = int(id)
	}
}

// Helper for killing a specific monster by NPC ID and type
func (s WindDruid) killMonster(npc npc.ID, t data.MonsterType) error {
	return s.KillMonsterSequence(func(d game.Data) (data.UnitID, bool) {
		m, found := d.Monsters.FindOne(npc, t)
		if !found {
			return 0, false
		}
		return m.UnitID, true
	}, nil)
}

// Reapplies active buffs if they've expired
func (s WindDruid) RecastBuffs() {
	ctx := context.Get()
	skills := []struct {
		id    skill.ID
		state state.State
	}{
		{skill.Hurricane, state.Hurricane},
		{skill.OakSage, state.Oaksage},
		{skill.CycloneArmor, state.Cyclonearmor},
	}

	for _, buff := range skills {
		if kb, found := ctx.Data.KeyBindings.KeyBindingForSkill(buff.id); found {
			if !ctx.Data.PlayerUnit.States.HasState(buff.state) { // Check if buff is missing
				// Hurricane requires both key press and right click to activate
				ctx.HID.PressKeyBinding(kb)               // Activate skill hotkey
				utils.Sleep(180)                          // Short delay for UI update
				ctx.HID.Click(game.RightButton, 640, 340) // Cast at screen center
				utils.Sleep(100)                          // Allow cast animation to complete
			}
		}
	}
}

// Return a list of available buff skills
func (s WindDruid) BuffSkills() []skill.ID {
	buffs := make([]skill.ID, 0)
	if _, found := s.Data.KeyBindings.KeyBindingForSkill(skill.CycloneArmor); found {
		buffs = append(buffs, skill.CycloneArmor)
	}
	if _, found := s.Data.KeyBindings.KeyBindingForSkill(skill.Raven); found {
		buffs = append(buffs, skill.Raven, skill.Raven, skill.Raven, skill.Raven, skill.Raven) // Summon 5 ravens
	}
	if _, found := s.Data.KeyBindings.KeyBindingForSkill(skill.Hurricane); found {
		buffs = append(buffs, skill.Hurricane)
	}
	return buffs
}

// Dynamically determines pre-combat buffs and summons
func (s WindDruid) PreCTABuffSkills() []skill.ID {
	// Initialize default needed counts
	needs := struct {
		bear       bool
		wolves     int
		direWolves int
		oak        bool
		creeper    bool
	}{
		bear:       true,
		wolves:     5,
		direWolves: 3,
		oak:        true,
		creeper:    true,
	}

	// Scan current pets and adjust needed counts
	for _, monster := range s.Data.Monsters {
		if monster.IsPet() {
			switch monster.Name {
			case npc.DruBear:
				needs.bear = false
			case npc.DruFenris:
				needs.direWolves = max(0, needs.direWolves-1)
			case npc.DruSpiritWolf:
				needs.wolves = max(0, needs.wolves-1)
			case npc.OakSage:
				needs.oak = false
			case npc.DruCycleOfLife, npc.VineCreature, npc.DruPlaguePoppy:
				needs.creeper = false
			}
		}
	}

	// Check active oak sage state
	if s.Data.PlayerUnit.States.HasState(state.Oaksage) {
		needs.oak = false
	}

	skills := make([]skill.ID, 0)

	// Add summoning skills based on need and key bindings
	if _, found := s.Data.KeyBindings.KeyBindingForSkill(skill.SummonSpiritWolf); found && needs.wolves > 0 {
		for i := 0; i < needs.wolves; i++ {
			skills = append(skills, skill.SummonSpiritWolf)
		}
	}

	// Add missing dire wolves (only needed quantity)
	if _, found := s.Data.KeyBindings.KeyBindingForSkill(skill.SummonDireWolf); found && needs.direWolves > 0 {
		for i := 0; i < needs.direWolves; i++ {
			skills = append(skills, skill.SummonDireWolf)
		}
	}

	// Add grizzly bear if missing
	if _, found := s.Data.KeyBindings.KeyBindingForSkill(skill.SummonGrizzly); found && needs.bear {
		skills = append(skills, skill.SummonGrizzly)
	}

	// Add oak sage if missing
	if _, found := s.Data.KeyBindings.KeyBindingForSkill(skill.OakSage); found && needs.oak {
		skills = append(skills, skill.OakSage)
	}

	// Add creepers if missing (only one type needed)
	if needs.creeper {
		if _, found := s.Data.KeyBindings.KeyBindingForSkill(skill.SolarCreeper); found {
			skills = append(skills, skill.SolarCreeper)
		} else if _, found := s.Data.KeyBindings.KeyBindingForSkill(skill.CarrionVine); found {
			skills = append(skills, skill.CarrionVine)
		} else if _, found := s.Data.KeyBindings.KeyBindingForSkill(skill.PoisonCreeper); found {
			skills = append(skills, skill.PoisonCreeper)
		}
	}

	return skills
}

func (s WindDruid) KillCountess() error {
	return s.killMonster(npc.DarkStalker, data.MonsterTypeSuperUnique)
}

func (s WindDruid) KillAndariel() error {
	return s.killMonster(npc.Andariel, data.MonsterTypeUnique)
}

func (s WindDruid) KillSummoner() error {
	return s.killMonster(npc.Summoner, data.MonsterTypeUnique)
}

func (s WindDruid) KillDuriel() error {
	return s.killMonster(npc.Duriel, data.MonsterTypeUnique)
}

// Targets multiple council members
func (s WindDruid) KillCouncil() error {
	context.Get().DisableItemPickup()
	defer context.Get().EnableItemPickup()

	for {
		if !s.anyCouncilMemberAlive() {
			break
		}

		err := s.KillMonsterSequence(func(d game.Data) (data.UnitID, bool) {
			// Find next alive council member
			for _, m := range d.Monsters.Enemies() {
				if (m.Name == npc.CouncilMember || m.Name == npc.CouncilMember2 || m.Name == npc.CouncilMember3) &&
					m.Stats[stat.Life] > 0 {
					return m.UnitID, true
				}
			}
			return 0, false
		}, nil)

		if err != nil {
			return err
		}
	}
	return nil
}

// Check if any council members are still alive
func (s WindDruid) anyCouncilMemberAlive() bool {
	for _, m := range s.Data.Monsters.Enemies() {
		if (m.Name == npc.CouncilMember || m.Name == npc.CouncilMember2 || m.Name == npc.CouncilMember3) &&
			m.Stats[stat.Life] > 0 {
			return true
		}
	}
	return false
}

func (s WindDruid) KillMephisto() error {
	return s.killMonster(npc.Mephisto, data.MonsterTypeUnique)
}

func (s WindDruid) KillIzual() error {
	return s.killMonster(npc.Izual, data.MonsterTypeUnique)
}

// KillDiablo includes a timeout and detection logic
func (s WindDruid) KillDiablo() error {
	timeout := time.Second * 20
	startTime := time.Now()
	diabloFound := false

	for {
		if time.Since(startTime) > timeout && !diabloFound {
			s.Logger.Error("Diablo was not found, timeout reached")
			return nil
		}

		diablo, found := s.Data.Monsters.FindOne(npc.Diablo, data.MonsterTypeUnique)
		if !found || diablo.Stats[stat.Life] <= 0 { // Check if Diablo is dead or missing
			// Diablo is dead
			if diabloFound {
				return nil
			}
			// Keep waiting..
			time.Sleep(200 * time.Millisecond)
			continue
		}

		diabloFound = true
		s.Logger.Info("Diablo detected, attacking")
		return s.killMonster(npc.Diablo, data.MonsterTypeUnique)
	}
}

func (s WindDruid) KillPindle() error {
	return s.killMonster(npc.DefiledWarrior, data.MonsterTypeSuperUnique)
}

func (s WindDruid) KillNihlathak() error {
	return s.killMonster(npc.Nihlathak, data.MonsterTypeSuperUnique)
}

func (s WindDruid) KillBaal() error {
	return s.killMonster(npc.BaalCrab, data.MonsterTypeUnique)
}
