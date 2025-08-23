package run

import (
	"fmt"

	"github.com/hectorgimenez/koolo/internal/action/step"

	"github.com/hectorgimenez/koolo/internal/game"
	"github.com/hectorgimenez/koolo/internal/ui"
	"github.com/hectorgimenez/koolo/internal/utils"

	"github.com/hectorgimenez/d2go/pkg/data"
	"github.com/hectorgimenez/d2go/pkg/data/area"
	"github.com/hectorgimenez/d2go/pkg/data/difficulty"
	"github.com/hectorgimenez/d2go/pkg/data/npc"
	"github.com/hectorgimenez/d2go/pkg/data/object"
	"github.com/hectorgimenez/d2go/pkg/data/quest"
	"github.com/hectorgimenez/d2go/pkg/data/stat"
	"github.com/hectorgimenez/koolo/internal/action"
	"github.com/hectorgimenez/koolo/internal/config" // Make sure this import is present
	//	"github.com/lxn/win"
)

func (a Leveling) act5() error {
	if a.ctx.Data.PlayerUnit.Area != area.Harrogath {
		return nil
	}

	action.VendorRefill(true, true)

	// Gold Farming Logic (and immediate return if farming is needed)
	if (a.ctx.CharacterCfg.Game.Difficulty == difficulty.Normal && a.ctx.Data.PlayerUnit.TotalPlayerGold() < 30000) ||
		(a.ctx.CharacterCfg.Game.Difficulty == difficulty.Nightmare && a.ctx.Data.PlayerUnit.TotalPlayerGold() < 50000) ||
		(a.ctx.CharacterCfg.Game.Difficulty == difficulty.Hell && a.ctx.Data.PlayerUnit.TotalPlayerGold() < 70000) {

		a.ctx.Logger.Info("Low on gold. Initiating Kurast Chest gold farm.")
		NewLowerKurastChest().Run()
		return action.WayPoint(area.Harrogath)
	}
	// If we reach this point, it means gold is sufficient, and we skip farming for this run.

	lvl, _ := a.ctx.Data.PlayerUnit.FindStat(stat.Level, 0)

	// Use a flag to indicate if difficulty was changed and needs saving
	difficultyChanged := false

	// Logic for Act5EveOfDestruction quest completion
	if a.ctx.Data.Quests[quest.Act5EveOfDestruction].Completed() {

		a.ctx.Logger.Info("Eve of Destruction completed")

		currentDifficulty := a.ctx.CharacterCfg.Game.Difficulty
		switch currentDifficulty {
		case difficulty.Normal:
			if lvl.Value >= 45 {
				a.ctx.CharacterCfg.Game.Difficulty = difficulty.Nightmare
				difficultyChanged = true
			}
		case difficulty.Nightmare:
			// Get current FireResist and LightningResist values using FindStat on PlayerUnit
			rawFireRes, _ := a.ctx.Data.PlayerUnit.FindStat(stat.FireResist, 0)
			rawLightRes, _ := a.ctx.Data.PlayerUnit.FindStat(stat.LightningResist, 0)

			// Apply Nightmare difficulty penalty (-40) to resistances for effective values
			effectiveFireRes := rawFireRes.Value - 40
			effectiveLightRes := rawLightRes.Value - 40

			// Log the effective resistance values
			a.ctx.Logger.Info(fmt.Sprintf("Current effective resistances (Nightmare penalty applied) - Fire: %d, Lightning: %d", effectiveFireRes, effectiveLightRes))

			// Check conditions using effective resistance values
			if lvl.Value >= 70 && effectiveFireRes >= 75 && effectiveLightRes >= 50 {
				a.ctx.CharacterCfg.Game.Difficulty = difficulty.Hell

				difficultyChanged = true
			}
		}

		if difficultyChanged {
			a.ctx.Logger.Info("Difficulty changed to %s. Saving character configuration...", a.ctx.CharacterCfg.Game.Difficulty)
			// Use the new ConfigFolderName field here!
			if err := config.SaveSupervisorConfig(a.ctx.CharacterCfg.ConfigFolderName, a.ctx.CharacterCfg); err != nil {
				a.ctx.Logger.Error("Failed to save character configuration: %s", err.Error())
				return fmt.Errorf("failed to save character configuration: %w", err)
			}
			return nil
		}
	}

	if a.ctx.CharacterCfg.Game.Difficulty == difficulty.Nightmare && lvl.Value < 60 {

		diabloRun := NewDiablo()
		err := diabloRun.Run()
		if err != nil {
			return err
		}
	}

	// Logic for Act5RiteOfPassage quest completion
	if a.ctx.Data.Quests[quest.Act5RiteOfPassage].Completed() && a.ctx.Data.Quests[quest.Act5PrisonOfIce].Completed() {
		a.ctx.Logger.Info("Starting Baal run...")
		if a.ctx.CharacterCfg.Game.Difficulty != difficulty.Normal {
			a.ctx.CharacterCfg.Game.Baal.SoulQuit = true
		}
		NewBaal(nil).Run()

		return nil
	}

	wp, _ := a.ctx.Data.Objects.FindOne(object.ExpansionWaypoint)
	action.MoveToCoords(wp.Position)

	if _, found := a.ctx.Data.Monsters.FindOne(npc.Drehya, data.MonsterTypeNone); !found {
		NewQuests().rescueAnyaQuest()

		action.MoveToCoords(data.Position{
			X: 5107,
			Y: 5119,
		})
		action.InteractNPC(npc.Drehya)

	}

	if a.ctx.Data.Quests[quest.Act5PrisonOfIce].HasStatus(quest.StatusInProgress6) {

		a.ctx.Logger.Info("StatusInProgress6")

		action.MoveToCoords(data.Position{
			X: 5107,
			Y: 5119,
		})
		action.InteractNPC(npc.Drehya)
	}

	if _, found := a.ctx.Data.Inventory.Find("ScrollOfResistance"); found {
		a.ctx.Logger.Info("ScrollOfResistance found in inventory, attempting to use it.")
		step.CloseAllMenus()
		a.ctx.HID.PressKeyBinding(a.ctx.Data.KeyBindings.Inventory)
		utils.Sleep(500) // Give time for inventory to open and data to refresh

		// Re-find the item after opening inventory to ensure correct screen position
		if itm, foundAgain := a.ctx.Data.Inventory.Find("ScrollOfResistance"); foundAgain {
			screenPos := ui.GetScreenCoordsForItem(itm)
			utils.Sleep(200)
			a.ctx.HID.Click(game.RightButton, screenPos.X, screenPos.Y)
			utils.Sleep(500) // Give time for the scroll to be used
			a.ctx.Logger.Info("ScrollOfResistance used.")
		} else {
			a.ctx.Logger.Warn("ScrollOfResistance disappeared from inventory before it could be used.")
		}
		step.CloseAllMenus() // Close inventory after attempt
	}

	/*	if lvl, _ := a.ctx.Data.PlayerUnit.FindStat(stat.Level, 0); lvl.Value < 35 && a.ctx.Data.CharacterCfg.Game.Difficulty == difficulty.Normal {
			return NewPindleskin().Run()


		}

		if lvl, _ := a.ctx.Data.PlayerUnit.FindStat(stat.Level, 0); lvl.Value < 60 && a.ctx.Data.CharacterCfg.Game.Difficulty == difficulty.Nightmare {
			return NewPindleskin().Run()
		}

		if lvl, _ := a.ctx.Data.PlayerUnit.FindStat(stat.Level, 0); lvl.Value < 80 && a.ctx.Data.CharacterCfg.Game.Difficulty == difficulty.Hell {
			return NewPindleskin().Run()
		}
	*/
	err := NewQuests().killAncientsQuest()
	if err != nil {
		return err
	}

	return nil
}

func (a Leveling) FrigidHighlands() error {
	a.ctx.Logger.Info("Entering BloodyFoothills for gold farming...")

	err := action.WayPoint(area.FrigidHighlands)
	if err != nil {
		a.ctx.Logger.Error("Failed to move to Frigid Highlands area: %v", err)
		return err
	}
	a.ctx.Logger.Info("Successfully reached Frigid Highlands.")

	err = action.ClearCurrentLevel(false, data.MonsterAnyFilter())
	if err != nil {
		a.ctx.Logger.Error("Failed to clear Frigid Highlands area: %v", err)
		return err
	}
	a.ctx.Logger.Info("Successfully cleared Frigid Highlands area.")

	return nil
}
