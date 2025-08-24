package run

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/hectorgimenez/d2go/pkg/data"
	"github.com/hectorgimenez/d2go/pkg/data/area"
	"github.com/hectorgimenez/d2go/pkg/data/difficulty"
	"github.com/hectorgimenez/d2go/pkg/data/npc"
	"github.com/hectorgimenez/d2go/pkg/data/quest"
	"github.com/hectorgimenez/d2go/pkg/data/stat"
	"github.com/hectorgimenez/koolo/internal/action"
	"github.com/hectorgimenez/koolo/internal/config"
	"github.com/hectorgimenez/koolo/internal/game"
	"github.com/hectorgimenez/koolo/internal/utils"

	"github.com/hectorgimenez/koolo/internal/context"
	"github.com/lxn/win"
)

// act1 is the main function for Act 1 leveling
func (a Leveling) act1() error {
	if a.ctx.Data.PlayerUnit.Area != area.RogueEncampment {
		return nil
	}

	// Check player level and set configuration for level 1
	lvl, _ := a.ctx.Data.PlayerUnit.FindStat(stat.Level, 0)
	if lvl.Value == 1 {
		a.ctx.Logger.Info("Player level is 1. Setting Leveling Config.")
		a.setupLevelOneConfig()
	}

	// Adjust belt and merc settings based on difficulty
	a.AdjustDifficultyConfig()

	// Refill potions and ensure bindings for players level > 1
	if lvl.Value > 1 {
		action.VendorRefill(true, true)
		if err := action.EnsureSkillBindings(); err != nil {
			a.ctx.Logger.Error("Error ensuring skill bindings after vendor refill: %s", err.Error())
		}
	}

	// --- Quest and Farming Logic ---

	// Farming for low gold
	if a.ctx.Data.PlayerUnit.TotalPlayerGold() < 50000 {
		if a.ctx.CharacterCfg.Game.Difficulty == difficulty.Nightmare {
			return a.stonyField()
		}
		if a.ctx.CharacterCfg.Game.Difficulty == difficulty.Hell {
			return NewMausoleum().Run()
		}
	}

	// Den of Evil quest
	//	if a.ctx.CharacterCfg.Game.Difficulty != difficulty.Hell &&  !a.ctx.Data.Quests[quest.Act1DenOfEvil].Completed() { {

	if !a.ctx.Data.Quests[quest.Act1DenOfEvil].Completed() && a.ctx.CharacterCfg.Game.Difficulty != difficulty.Hell {
		a.ctx.Logger.Debug("Completing Den of Evil")
		return NewQuests().clearDenQuest()
	}

	// Farming for normal difficulty below 300 gold
	if a.ctx.CharacterCfg.Game.Difficulty == difficulty.Normal && a.ctx.Data.PlayerUnit.TotalPlayerGold() < 300 && !a.ctx.Data.Quests[quest.Act1SistersBurialGrounds].Completed() {
		return NewTristramEarlyGoldfarm().Run()
	}

	// Blood Raven quest
	if a.ctx.CharacterCfg.Game.Difficulty == difficulty.Normal && !a.ctx.Data.Quests[quest.Act1SistersBurialGrounds].Completed() {
		return a.killRavenGetMerc()
	}

	if a.ctx.CharacterCfg.Character.UseMerc == false && a.ctx.Data.Quests[quest.Act1SistersBurialGrounds].Completed() {
		a.ctx.CharacterCfg.Character.UseMerc = true

		action.InteractNPC(npc.Kashya)
		if err := config.SaveSupervisorConfig(a.ctx.CharacterCfg.ConfigFolderName, a.ctx.CharacterCfg); err != nil {
			a.ctx.Logger.Error("Failed to save character configuration: %s", err.Error())

		}
	}

	// Cain quest: entering Tristram
	if (a.ctx.Data.Quests[quest.Act1TheSearchForCain].HasStatus(quest.StatusInProgress2) || a.ctx.Data.Quests[quest.Act1TheSearchForCain].HasStatus(quest.StatusInProgress3) || a.ctx.Data.Quests[quest.Act1TheSearchForCain].HasStatus(quest.StatusInProgress4)) && a.ctx.CharacterCfg.Game.Difficulty != difficulty.Hell {
		return NewTristram().Run()
	}

	// Farming for normal difficulty below 400 gold
	if a.ctx.CharacterCfg.Game.Difficulty == difficulty.Normal && a.ctx.Data.PlayerUnit.TotalPlayerGold() < 400 && !a.isCainInTown() && !a.ctx.Data.Quests[quest.Act1TheSearchForCain].Completed() {
		return NewTristramEarlyGoldfarm().Run()
	}

	/*	if a.ctx.CharacterCfg.Game.Difficulty == difficulty.Normal && lvl.Value < 6 {
		return NewTristramEarlyGoldfarm().Run()
	}*/

	// Cain quest: talking to Akara
	if !a.isCainInTown() && !a.ctx.Data.Quests[quest.Act1TheSearchForCain].Completed() && a.ctx.CharacterCfg.Game.Difficulty != difficulty.Hell {
		a.ctx.CharacterCfg.Character.ClearPathDist = 7
		if err := config.SaveSupervisorConfig(a.ctx.CharacterCfg.ConfigFolderName, a.ctx.CharacterCfg); err != nil {
			a.ctx.Logger.Error("Failed to save character configuration: %s", err.Error())
		}
		return NewQuests().rescueCainQuest()
	}

	// Tristram only until lvl 6, then Trist + Act1 Progression (good exp, less town chores)
	if a.ctx.CharacterCfg.Game.Difficulty == difficulty.Normal && lvl.Value < 12 {

		a.ctx.CharacterCfg.Character.ClearPathDist = 4
		if err := config.SaveSupervisorConfig(a.ctx.CharacterCfg.ConfigFolderName, a.ctx.CharacterCfg); err != nil {
			a.ctx.Logger.Error("Failed to save character configuration: %s", err.Error())
		}

		if lvl.Value < 12 {
			// Run Tristram and end the function
			return NewTristram().Run()
		} else {

			NewTristram().Run()

		}

	}

	// Andariel or Act 2 transition
	if a.ctx.Data.Quests[quest.Act1SistersToTheSlaughter].Completed() {
		// Go to Act 2
		return a.goToAct2()
	} else {
		// Run Andariel to complete quest

		if a.ctx.CharacterCfg.Game.Difficulty == difficulty.Normal {

		a.ctx.CharacterCfg.Character.ClearPathDist = 7
		a.ctx.CharacterCfg.Inventory.BeltColumns = [4]string{"healing", "healing", "mana", "mana"}
		if err := config.SaveSupervisorConfig(a.ctx.CharacterCfg.ConfigFolderName, a.ctx.CharacterCfg); err != nil {
			a.ctx.Logger.Error("Failed to save character configuration: %s", err.Error())

		}}
		return NewAndariel().Run()
	}
	return nil
}

// setupLevelOneConfig centralizes the configuration logic for a new character.
func (a Leveling) setupLevelOneConfig() {
	a.ctx.CharacterCfg.Game.Difficulty = difficulty.Normal
	a.ctx.CharacterCfg.Game.Leveling.EnsurePointsAllocation = true
	a.ctx.CharacterCfg.Game.Leveling.EnsureKeyBinding = true
	a.ctx.CharacterCfg.Game.Leveling.AutoEquip = true
	a.ctx.CharacterCfg.Game.Leveling.EnableRunewordMaker = true
	a.ctx.CharacterCfg.Game.Leveling.EnabledRunewordRecipes = []string{"Ancients' Pledge", "Lore", "Insight", "Spirit", "Smoke", "Treachery", "Heart of the Oak", "Call to Arms"}
	a.ctx.CharacterCfg.Character.UseTeleport = true
	a.ctx.CharacterCfg.Character.UseMerc = false
	a.ctx.CharacterCfg.Game.UseCainIdentify = true
	a.ctx.CharacterCfg.CloseMiniPanel = false
	a.ctx.CharacterCfg.Health.HealingPotionAt = 40
	a.ctx.CharacterCfg.Health.ManaPotionAt = 25
	a.ctx.CharacterCfg.Health.RejuvPotionAtLife = 0
	a.ctx.CharacterCfg.Health.ChickenAt = 7
	a.ctx.CharacterCfg.Gambling.Enabled = true
	a.ctx.CharacterCfg.Character.ClearPathDist = 7
	a.ctx.CharacterCfg.Health.MercRejuvPotionAt = 40
	a.ctx.CharacterCfg.Health.MercChickenAt = 0
	a.ctx.CharacterCfg.Health.MercHealingPotionAt = 25
	a.ctx.CharacterCfg.MaxGameLength = 1200
	a.ctx.CharacterCfg.CubeRecipes.Enabled = true
	a.ctx.CharacterCfg.CubeRecipes.EnabledRecipes = []string{"Perfect Amethyst", "Reroll GrandCharms", "Caster Amulet"}
	a.ctx.CharacterCfg.Inventory.BeltColumns = [4]string{"healing", "healing", "mana", "mana"}
	a.ctx.CharacterCfg.BackToTown.NoHpPotions = true
	a.ctx.CharacterCfg.BackToTown.NoMpPotions = true
	a.ctx.CharacterCfg.BackToTown.MercDied = false
	a.ctx.CharacterCfg.BackToTown.EquipmentBroken = false
	a.ctx.CharacterCfg.Game.Tristram.ClearPortal = false
	a.ctx.CharacterCfg.Game.Tristram.FocusOnElitePacks = true
	a.ctx.CharacterCfg.Game.Pit.MoveThroughBlackMarsh = true
	a.ctx.CharacterCfg.Game.Pit.OpenChests = true
	a.ctx.CharacterCfg.Game.Pit.FocusOnElitePacks = false
	a.ctx.CharacterCfg.Game.Pit.OnlyClearLevel2 = false
	a.ctx.CharacterCfg.Game.Andariel.ClearRoom = true
	a.ctx.CharacterCfg.Game.Andariel.UseAntidoes = true
	a.ctx.CharacterCfg.Game.Mephisto.KillCouncilMembers = false
	a.ctx.CharacterCfg.Game.Mephisto.OpenChests = false
	a.ctx.CharacterCfg.Game.Mephisto.ExitToA4 = true
	a.ctx.CharacterCfg.Inventory.InventoryLock = [][]int{
		{1, 1, 1, 1, 1, 1, 1, 1, 1, 1},
		{1, 1, 1, 1, 1, 1, 1, 1, 1, 1},
		{1, 1, 1, 1, 1, 1, 1, 1, 1, 1},
		{1, 1, 1, 1, 1, 1, 1, 1, 1, 1},
	}
	if err := config.SaveSupervisorConfig(a.ctx.CharacterCfg.ConfigFolderName, a.ctx.CharacterCfg); err != nil {
		a.ctx.Logger.Error("Failed to save character configuration: %s", err.Error())
	}
}

// adjustDifficultyConfig centralizes difficulty-based configuration changes.
func (a Leveling) AdjustDifficultyConfig() {
	lvl, _ := a.ctx.Data.PlayerUnit.FindStat(stat.Level, 0)
	if lvl.Value >= 4 && lvl.Value < 12 {
		a.ctx.CharacterCfg.Health.HealingPotionAt = 85
		a.ctx.CharacterCfg.Character.ClearPathDist = 7
	}
	if lvl.Value >= 24 {
		if a.ctx.CharacterCfg.Game.Difficulty == difficulty.Normal {
			a.ctx.CharacterCfg.Inventory.BeltColumns = [4]string{"healing", "healing", "mana", "mana"}
			a.ctx.CharacterCfg.Health.MercHealingPotionAt = 55
			a.ctx.CharacterCfg.Health.MercRejuvPotionAt = 0
			a.ctx.CharacterCfg.Health.HealingPotionAt = 85
			a.ctx.CharacterCfg.Health.ChickenAt = 30
			a.ctx.CharacterCfg.Character.ClearPathDist = 0

		} else if a.ctx.CharacterCfg.Game.Difficulty == difficulty.Nightmare {
			a.ctx.CharacterCfg.Inventory.BeltColumns = [4]string{"healing", "healing", "mana", "mana"}
			a.ctx.CharacterCfg.Health.MercHealingPotionAt = 55
			a.ctx.CharacterCfg.Health.MercRejuvPotionAt = 0
			a.ctx.CharacterCfg.Health.HealingPotionAt = 85
			a.ctx.CharacterCfg.Health.ChickenAt = 30
			a.ctx.CharacterCfg.Character.ClearPathDist = 0

		} else if a.ctx.CharacterCfg.Game.Difficulty == difficulty.Hell {
			a.ctx.CharacterCfg.Inventory.BeltColumns = [4]string{"healing", "healing", "mana", "rejuvenation"}
			a.ctx.CharacterCfg.Health.MercHealingPotionAt = 80
			a.ctx.CharacterCfg.Health.MercRejuvPotionAt = 40
			a.ctx.CharacterCfg.Health.MercHealingPotionAt = 80
			a.ctx.CharacterCfg.Health.HealingPotionAt = 90
			a.ctx.CharacterCfg.Health.ChickenAt = 0

		}
		if err := config.SaveSupervisorConfig(a.ctx.CharacterCfg.ConfigFolderName, a.ctx.CharacterCfg); err != nil {
			a.ctx.Logger.Error("Failed to save character configuration: %s", err.Error())
		}
	}
}

// goToAct2 handles the transition to Act 2.
func (a Leveling) goToAct2() error {
	a.ctx.Logger.Info("Act 1 completed. Moving to Act 2.")
	action.ReturnTown()

	// Do Den of Evil if not complete before moving acts
	if !a.ctx.Data.Quests[quest.Act1DenOfEvil].Completed() && a.ctx.CharacterCfg.Game.Difficulty != difficulty.Hell {
		if err := NewQuests().clearDenQuest(); err != nil {
			return err
		}
	}
	// Rescue Cain if not already done
	if !a.isCainInTown() && a.ctx.CharacterCfg.Game.Difficulty != difficulty.Hell {
		if err := NewQuests().rescueCainQuest(); err != nil {
			return err
		}
	}

	action.InteractNPC(npc.Warriv)
	a.ctx.HID.KeySequence(win.VK_HOME, win.VK_DOWN, win.VK_RETURN)
	utils.Sleep(1000)
	a.HoldKey(win.VK_ESCAPE, 2000)
	utils.Sleep(1000)
	return nil
}

// coldPlains handles clearing Cold Plains
func (a Leveling) coldPlains() error {
	err := action.WayPoint(area.ColdPlains)
	if err != nil {
		return err
	}
	return action.ClearCurrentLevel(false, data.MonsterAnyFilter())
}

// stonyField handles clearing Stony Field
func (a Leveling) stonyField() error {
	err := action.WayPoint(area.StonyField)
	if err != nil {
		return err
	}
	return action.ClearCurrentLevel(false, data.MonsterAnyFilter())
}

// isCainInTown checks if Deckard Cain is in town
func (a Leveling) isCainInTown() bool {
	_, found := a.ctx.Data.Monsters.FindOne(npc.DeckardCain5, data.MonsterTypeNone)
	return found
}

// killRavenGetMerc efficiently finds and kills Blood Raven by pathing near the Mausoleum entrance.
func (a Leveling) killRavenGetMerc() error {
	ctx := a.ctx
	ctx.SetLastAction("killRavenGetMerc")

	// Use a timeout for the entire process to prevent infinite loops
	timeout := time.Now().Add(5 * time.Minute)

	// Step 1: Use the waypoint to travel to Cold Plains and then move to Burial Grounds.
	if ctx.Data.PlayerUnit.Area != area.BurialGrounds {
		err := action.WayPoint(area.ColdPlains)
		if err != nil {
			return fmt.Errorf("failed to move to Cold Plains: %w", err)
		}
		if err = action.MoveToArea(area.BurialGrounds); err != nil {
			return fmt.Errorf("failed to move to Burial Grounds: %w", err)
		}
	}

	// Step 2: Loop to find and kill Blood Raven, or until timeout.
	for time.Now().Before(timeout) {
		bloodRaven, found := ctx.Data.Monsters.FindOne(npc.BloodRaven, data.MonsterTypeNone)

		// If Blood Raven is found and her health is 0, she's dead. We can break the loop.
		if found && bloodRaven.Stats[stat.Life] <= 0 {
			ctx.Logger.Info("Blood Raven is confirmed dead. Returning to town.")
			break
		}

		originalBackToTownCfg := a.ctx.CharacterCfg.BackToTown

		a.ctx.CharacterCfg.BackToTown.NoMpPotions = false
		a.ctx.CharacterCfg.Health.HealingPotionAt = 55

		defer func() {
			a.ctx.CharacterCfg.BackToTown = originalBackToTownCfg
			a.ctx.Logger.Info("Restored original back-to-town checks after Mephisto fight.")
		}()

		// If she's not found, try to move to a good position to force the area to load
		// and reveal her.
		if !found {
			mausoleumEntranceLvl, entranceFound := findMausoleumEntrance(ctx)
			if entranceFound {
				targetPos := atDistance(mausoleumEntranceLvl.Position, ctx.Data.PlayerUnit.Position, 15)
				if err := action.MoveToCoords(targetPos); err != nil {
					return fmt.Errorf("failed to move to position near Mausoleum entrance: %w", err)
				}
				ctx.Logger.Info("Moved to a strategic position to search for Blood Raven.")
			} else {
				// If the entrance isn't found, clear the area to force load
				ctx.Logger.Info("Mausoleum entrance not found. Clearing area to reveal Blood Raven.")
				if err := action.ClearCurrentLevel(false, data.MonsterAnyFilter()); err != nil {
					ctx.Logger.Warn("Failed to clear area: %s", err.Error())
				}
			}
			time.Sleep(1 * time.Second)
			continue
		}

		// If she is found and alive, engage her.
		ctx.Logger.Info("Blood Raven found, engaging.")
		if err := a.killBloodRaven(bloodRaven); err != nil {
			ctx.Logger.Warn("Failed to kill Blood Raven: %s. Retrying...", err.Error())
			time.Sleep(1 * time.Second)
			continue
		}

		time.Sleep(500 * time.Millisecond)
	}

	// If the loop timed out, return an error.
	if time.Now().After(timeout) {
		return errors.New("Blood Raven quest failed to complete within the timeout period")
	}

	return nil
}

// A helper function to encapsulate the fallback logic.
func (a Leveling) fallbackClear() error {
	err := action.ClearCurrentLevel(false, data.MonsterAnyFilter())
	if err != nil {
		return fmt.Errorf("failed to clear Burial Grounds (fallback): %w", err)
	}

	return nil
}

// A helper function to encapsulate the kill logic, without quest checks.
func (a Leveling) killBloodRaven(bloodRaven data.Monster) error {
	ctx := a.ctx
	err := ctx.Char.KillMonsterSequence(func(d game.Data) (data.UnitID, bool) {
		m, monsterFound := d.Monsters.FindByID(bloodRaven.UnitID)
		if monsterFound && m.Stats[stat.Life] > 0 {
			return bloodRaven.UnitID, true
		}
		return 0, false
	}, nil)

	if err != nil {
		return fmt.Errorf("failed to kill Blood Raven: %w", err)
	}

	return nil
}

/* A helper function to encapsulate the quest completion logic.
func (a Leveling) completeQuest() error {
	action.ReturnTown()
	err := action.InteractNPC(npc.Kashya)
	if err != nil {
		return fmt.Errorf("failed to interact with Kashya: %w", err)
	}
	return nil
}*/

// atDistance is a helper function to calculate a position a certain distance away from a target.
func atDistance(start, end data.Position, distance int) data.Position {
	dx := float64(end.X - start.X)
	dy := float64(end.Y - start.Y)
	dist := math.Sqrt(dx*dx + dy*dy)

	if dist == 0 {
		return start
	}

	ratio := float64(distance) / dist
	newX := float64(start.X) + dx*ratio
	newY := float64(start.Y) + dy*ratio

	return data.Position{X: int(newX), Y: int(newY)}
}

// A helper to simplify the main function
func findMausoleumEntrance(ctx *context.Status) (data.Level, bool) {
	for _, adjLvl := range ctx.Data.AdjacentLevels {
		if adjLvl.Area == area.Mausoleum {
			return adjLvl, true
		}
	}
	return data.Level{}, false
}
