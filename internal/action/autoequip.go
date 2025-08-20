package action

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/hectorgimenez/d2go/pkg/data"
	"github.com/hectorgimenez/d2go/pkg/data/item"
	"github.com/hectorgimenez/d2go/pkg/data/npc"
	"github.com/hectorgimenez/d2go/pkg/data/stat"
	"github.com/hectorgimenez/koolo/internal/action/step"
	"github.com/hectorgimenez/koolo/internal/context"
	"github.com/hectorgimenez/koolo/internal/game"
	"github.com/hectorgimenez/koolo/internal/ui"
	"github.com/hectorgimenez/koolo/internal/utils"
)

// Constants for equipment locations
const (
	EquipDelayMS = 500
	MaxRetries   = 2
)

var (
	classItems = map[data.Class][]string{
		data.Amazon:      {"ajav", "abow", "aspe"},
		data.Sorceress:   {"orb"},
		data.Necromancer: {"head"},
		data.Paladin:     {"ashd"},
		data.Barbarian:   {"phlm"},
		data.Druid:       {"pelt"},
		data.Assassin:    {"h2h"},
	}

	// shieldTypes defines items that should be equipped in right arm (technically they can be left or right arm but we don't want to try and equip two shields)
	shieldTypes = []string{"shie", "ashd", "head"}

	// mercBodyLocs defines valid mercenary equipment locations
	// No support for A3 and A5 mercs
	mercBodyLocs = []item.LocationType{item.LocHead, item.LocTorso, item.LocLeftArm}

	// questItems defines items that shouldn't be equipped
	// TODO Fix IsFromQuest() and remove
	questItems = []item.Name{
		"StaffOfKings",
		"HoradricStaff",
		"AmuletOfTheViper",
		"KhalimsFlail",
	}

	ErrFailedToEquip = errors.New("failed to equip item, quitting game")
)

// EvaluateAllItems evaluates and equips items for both player and mercenary
func AutoEquip() error {
	ctx := context.Get()

	for i := 0; i < MaxRetries; i++ {
		allItems := ctx.Data.Inventory.ByLocation(
			item.LocationStash,
			item.LocationInventory,
			item.LocationEquipped,
			item.LocationMercenary,
		)

		playerItems := evaluateItems(allItems, item.LocationEquipped, PlayerScore)
		if err := equipBestItems(playerItems, item.LocationEquipped); err != nil {
			ctx.Logger.Error(fmt.Sprintf("Failed to equip player items: %v", err))
			continue
		}

		*ctx.Data = ctx.GameReader.GetData()

		allItems = ctx.Data.Inventory.ByLocation(
			item.LocationStash,
			item.LocationInventory,
			item.LocationEquipped,
			item.LocationMercenary,
		)

		mercItems := evaluateItems(allItems, item.LocationMercenary, MercScore)
		if ctx.Data.MercHPPercent() > 0 {
			if err := equipBestItems(mercItems, item.LocationMercenary); err != nil {
				ctx.Logger.Error(fmt.Sprintf("Failed to equip mercenary items: %v", err))
				continue
			}
		}

		*ctx.Data = ctx.GameReader.GetData()
		if isEquipmentStable(playerItems, mercItems) {
			ctx.Logger.Debug("All items equipped as planned, no more changes needed.")
			return nil
		}
	}

	return fmt.Errorf("failed to equip all best items after multiple retries")
}

// isEquipmentStable checks if the currently equipped items match the top-ranked items from the last evaluation.
func isEquipmentStable(playerItems, mercItems map[item.LocationType][]data.Item) bool {
	ctx := context.Get()
	isStable := true

	// Check player equipment
	for loc, items := range playerItems {
		if len(items) > 0 {
			bestItem := items[0]
			equippedItem := GetEquippedItem(ctx.Data.Inventory, loc)
			if equippedItem.UnitID == 0 || equippedItem.UnitID != bestItem.UnitID {
				ctx.Logger.Debug(fmt.Sprintf("Player equipment unstable at %s. Best item is %s, but equipped is %s", loc, bestItem.Name, equippedItem.Name))
				isStable = false
			}
		}
	}

	// Check mercenary equipment
	for loc, items := range mercItems {
		if len(items) > 0 {
			bestItem := items[0]
			equippedItem := GetMercEquippedItem(ctx.Data.Inventory, loc)
			if equippedItem.UnitID == 0 || equippedItem.UnitID != bestItem.UnitID {
				ctx.Logger.Debug(fmt.Sprintf("Mercenary equipment unstable at %s. Best item is %s, but equipped is %s", loc, bestItem.Name, equippedItem.Name))
				isStable = false
			}
		}
	}

	return isStable
}

// isEquippable checks if an item meets the requirements for the given unit (player or NPC)
func isEquippable(i data.Item, target item.LocationType) bool {
	ctx := context.Get()

	bodyLoc := i.Desc().GetType().BodyLocs
	if len(bodyLoc) == 0 {
		return false
	}

	var str, dex, lvl int
	if target == item.LocationEquipped {
		str = ctx.Data.PlayerUnit.Stats[stat.Strength].Value
		dex = ctx.Data.PlayerUnit.Stats[stat.Dexterity].Value
		lvl = ctx.Data.PlayerUnit.Stats[stat.Level].Value
	} else if target == item.LocationMercenary {
		for _, m := range ctx.Data.Monsters {
			if m.IsMerc() {
				str = m.Stats[stat.Strength]
				dex = m.Stats[stat.Dexterity]
				lvl = m.Stats[stat.Level]
			}
		}
	}

	isQuestItem := slices.Contains(questItems, i.Name)

	for class, items := range classItems {
		if ctx.Data.PlayerUnit.Class != class && slices.Contains(items, i.Desc().Type) {
			return false
		}
	}

	isBowOrXbow := i.Desc().Type == "bow" || i.Desc().Type == "xbow" || i.Desc().Type == "bowq" || i.Desc().Type == "xbowq"
	isAmazon := ctx.Data.PlayerUnit.Class == data.Amazon

	// New rule: disallow 2-handed weapons for player level > 11
	if _, isTwoHanded := i.FindStat(stat.TwoHandedMinDamage, 0); isTwoHanded {
		if target == item.LocationEquipped && ctx.Data.PlayerUnit.Stats[stat.Level].Value > 11 {
			return false
		}
	}

	if target == item.LocationEquipped && isBowOrXbow && !isAmazon {
		return false
	}

	return i.Identified &&
		str >= i.Desc().RequiredStrength &&
		dex >= i.Desc().RequiredDexterity &&
		lvl >= i.LevelReq &&
		!isQuestItem
}

func isValidLocation(i data.Item, bodyLoc item.LocationType, target item.LocationType) bool {
	ctx := context.Get()
	class := ctx.Data.PlayerUnit.Class
	itemType := i.Desc().Type
	isShield := slices.Contains(shieldTypes, string(itemType))

	if target == item.LocationMercenary {
		if slices.Contains(mercBodyLocs, bodyLoc) {
			if bodyLoc == item.LocLeftArm {
				if isAct2MercenaryPresent(npc.Guard) {
					return itemType == "spea" || itemType == "pole" || itemType == "jave"
				} else {
					return itemType == "bow"
				}
			}
			return true
		}
		return false
	}

	if target == item.LocationEquipped {
		if isShield {
			return bodyLoc == item.LocRightArm
		}

		if bodyLoc != item.LocRightArm {
			return true
		}

		switch class {
		case data.Barbarian:
			_, isOneHanded := i.FindStat(stat.MaxDamage, 0)
			_, isTwoHanded := i.FindStat(stat.TwoHandedMaxDamage, 0)
			return isOneHanded || (isTwoHanded && itemType == "swor")

		case data.Assassin:
			isClaws := itemType == "h2h" || itemType == "h2h2"

			if isClaws && bodyLoc == item.LocRightArm {
				for _, equippedItem := range ctx.Data.Inventory.ByLocation(item.LocationEquipped) {
					if equippedItem.Location.BodyLocation == item.LocLeftArm {
						return equippedItem.Desc().Type == "h2h" || equippedItem.Desc().Type == "h2h2"
					}
				}
				return false
			}
			return isClaws
		default:
			return false
		}
	}

	return false
}

// isAct2MercenaryPresent checks for the existence of an Act 2 mercenary
func isAct2MercenaryPresent(mercName npc.ID) bool {
	ctx := context.Get()
	for _, monster := range ctx.Data.Monsters {
		if monster.IsMerc() && monster.Name == mercName {
			ctx.Logger.Debug(fmt.Sprintf("Mercenary of type %v is already present.", mercName))
			return true
		}
	}
	return false
}

// evaluateItems processes items for either player or merc
func evaluateItems(items []data.Item, target item.LocationType, scoreFunc func(data.Item) map[item.LocationType]float64) map[item.LocationType][]data.Item {
	ctx := context.Get()
	itemsByLoc := make(map[item.LocationType][]data.Item)
	itemScores := make(map[data.UnitID]map[item.LocationType]float64)

	for _, itm := range items {

		if !isEquippable(itm, target) {
			continue
		}

		if itm.Desc().Name == "Bolts" || itm.Desc().Name == "Arrows" || itm.Desc().Type == "thro" || itm.Desc().Type == "thrq" || itm.Desc().Type == "tkni" || itm.Desc().Type == "taxe" || itm.Desc().Type == "tpot" {
			continue
		}

		bodyLocScores := scoreFunc(itm)

		if len(bodyLocScores) > 0 {
			if _, exists := itemScores[itm.UnitID]; !exists {
				itemScores[itm.UnitID] = make(map[item.LocationType]float64)
			}

			for bodyLoc, score := range bodyLocScores {
				isValid := isValidLocation(itm, bodyLoc, target)

				if isValid {
					itemScores[itm.UnitID][bodyLoc] = score
					itemsByLoc[bodyLoc] = append(itemsByLoc[bodyLoc], itm)
				}
			}
		}
	}

	for loc := range itemsByLoc {
		sort.Slice(itemsByLoc[loc], func(i, j int) bool {
			scoreI := itemScores[itemsByLoc[loc][i].UnitID][loc]
			scoreJ := itemScores[itemsByLoc[loc][j].UnitID][loc]
			return scoreI > scoreJ
		})

		ctx.Logger.Debug(fmt.Sprintf("*** Sorted items for %s ***", loc))
		for i, itm := range itemsByLoc[loc] {
			score := itemScores[itm.UnitID][loc]
			ctx.Logger.Debug(fmt.Sprintf("%d. %s (Score: %.1f)", i+1, itm.IdentifiedName, score))
		}
		ctx.Logger.Debug("**********************************")
	}

	if target == item.LocationEquipped {
		class := ctx.Data.PlayerUnit.Class

		if items, ok := itemsByLoc[item.LocLeftArm]; ok && len(items) > 0 {
			if _, found := items[0].FindStat(stat.TwoHandedMinDamage, 0); found {
				if class == data.Barbarian && items[0].Desc().Type == "swor" {
				} else {
					var bestComboScore float64
					for _, itm := range items {
						if _, isTwoHanded := itm.FindStat(stat.TwoHandedMinDamage, 0); !isTwoHanded {
							if score, exists := itemScores[itm.UnitID]["left_arm"]; exists {
								ctx.Logger.Debug(fmt.Sprintf("Best one-handed weapon score: %.1f", score))
								bestComboScore = score
								break
							}
						}
					}

					if rightArmItems, ok := itemsByLoc[item.LocRightArm]; ok && len(rightArmItems) > 0 {
						if score, exists := itemScores[rightArmItems[0].UnitID][item.LocRightArm]; exists {
							ctx.Logger.Debug(fmt.Sprintf("Best shield score: %.1f", score))
							bestComboScore += score
							ctx.Logger.Debug(fmt.Sprintf("Best one-hand + shield combo score: %.1f", bestComboScore))
						}
					}

					if twoHandedScore, exists := itemScores[items[0].UnitID][item.LocLeftArm]; exists && bestComboScore >= twoHandedScore {
						ctx.Logger.Debug(fmt.Sprintf("Removing two-handed weapon: %s", items[0].Name))
						itemsByLoc[item.LocLeftArm] = itemsByLoc[item.LocLeftArm][1:]
					}
				}
			}
		}
	}

	return itemsByLoc
}

// equipBestItems equips the highest scoring items for each location, with retries
func equipBestItems(itemsByLoc map[item.LocationType][]data.Item, target item.LocationType) error {
	ctx := context.Get()

	equippedItems := make(map[data.UnitID]bool)

	for loc, items := range itemsByLoc {
		if len(items) == 0 {
			continue
		}

		isBestItemEquipped := false
		currentlyEquipped := GetEquippedItem(ctx.Data.Inventory, loc)
		if currentlyEquipped.UnitID != 0 && items[0].UnitID == currentlyEquipped.UnitID {
			isBestItemEquipped = true
		}

		if isBestItemEquipped {
			ctx.Logger.Debug(fmt.Sprintf("Best item %s for %s is already equipped. Skipping.", items[0].Name, loc))
			continue
		}

		// Flag to track if at least one item was successfully equipped for this location
		itemEquippedForLoc := false

		for _, itm := range items {

			if itm.Location.LocationType == target {
				break
			}

			if equippedItems[itm.UnitID] {
				ctx.Logger.Debug(fmt.Sprintf("Skipping %s for %s as it was already equipped elsewhere", itm.Name, loc))
				continue
			}

			if (itm.Location.LocationType == item.LocationMercenary && target == item.LocationEquipped) || (itm.Location.LocationType == item.LocationEquipped && target == item.LocationMercenary) {
				continue
			}

			var equipErr error
			for i := 0; i < MaxRetries; i++ {
				ctx.Logger.Debug(fmt.Sprintf("Attempting to equip %s to %s (Attempt %d/%d)", itm.Name, loc, i+1, MaxRetries))
				equipErr = equip(itm, loc, target)
				if equipErr == nil {
					ctx.Logger.Debug(fmt.Sprintf("Successfully equipped %s to %s", itm.Name, loc))
					itemEquippedForLoc = true
					break
				}
				ctx.Logger.Warn(fmt.Sprintf("Failed to equip %s, retrying...", itm.Name))
				time.Sleep(1 * time.Second)
			}

			if equipErr != nil {
				ctx.Logger.Error(fmt.Sprintf("Failed to equip %s after %d attempts. Considering it junk and attempting to sell all junk.", itm.Name, MaxRetries))

				err := VendorRefill(false, true)
				if err != nil {
					return fmt.Errorf("failed to equip item and failed to sell junk: %w", err)
				}

				ctx.Logger.Info(fmt.Sprintf("Successfully triggered junk sale. Hope item %s is gone.", itm.Name))

				// We can now safely continue to the next item in the list
				continue
			}

			// If we successfully equipped an item, we can break out of the inner loop and move to the next location
			if itemEquippedForLoc {
				equippedItems[itm.UnitID] = true
				break
			}
		}

		// If after checking all items for a location, none could be equipped, return an error
		if !itemEquippedForLoc {
			return fmt.Errorf("failed to equip any item for location %s", loc)
		}
	}

	return nil
}

// passing in bodyloc as a parameter cos rings have 2 locations
func equip(itm data.Item, bodyloc item.LocationType, target item.LocationType) error {

	ctx := context.Get()
	ctx.SetLastAction("Equip")

	// Ensure all menus are closed when the function exits
	defer step.CloseAllMenus()

	itemCoords := ui.GetScreenCoordsForItem(itm)

	if itm.Location.LocationType == item.LocationStash || itm.Location.LocationType == item.LocationSharedStash {
		OpenStash()
		utils.Sleep(EquipDelayMS)
		switch itm.Location.LocationType {
		case item.LocationStash:
			SwitchStashTab(1)
		case item.LocationSharedStash:
			SwitchStashTab(itm.Location.Page + 1)
		}

		if target == item.LocationMercenary {

			if itemFitsInventory(itm) {
				ctx.HID.ClickWithModifier(game.LeftButton, itemCoords.X, itemCoords.Y, game.CtrlKey)

				utils.Sleep(EquipDelayMS)
				*ctx.Data = ctx.GameReader.GetData()

				inInventory := false
				for _, updatedItem := range ctx.Data.Inventory.AllItems {
					if itm.UnitID == updatedItem.UnitID {
						itemCoords = ui.GetScreenCoordsForItem(updatedItem)
						inInventory = true
						break
					}
				}
				if !inInventory || !itemFitsInventory(itm) {
					return fmt.Errorf("item not found in inventory after moving from stash")
				}
				utils.Sleep(EquipDelayMS)

				// Close all menus after moving the item to the inventory to prevent getting stuck
				step.CloseAllMenus()
				utils.Sleep(EquipDelayMS)
			}
		}
	}

	for !ctx.Data.OpenMenus.Inventory {
		ctx.HID.PressKeyBinding(ctx.Data.KeyBindings.Inventory)
		utils.Sleep(EquipDelayMS)
	}
	if target == item.LocationMercenary {
		ctx.HID.PressKeyBinding(ctx.Data.KeyBindings.MercenaryScreen)
		utils.Sleep(EquipDelayMS)
	}

	itemToEquip := FindItemByUnitID(ctx.Data.Inventory, itm.UnitID)
	if itemToEquip.UnitID == 0 {
		return fmt.Errorf("item disappeared from inventory before equipping")
	}
	itemCoords = ui.GetScreenCoordsForItem(itemToEquip)

	if target == item.LocationMercenary {
		ctx.HID.ClickWithModifier(game.LeftButton, itemCoords.X, itemCoords.Y, game.CtrlKey)
	} else {
		switch bodyloc {
		case item.LocRightRing:
			if !itemFitsInventory(itm) {
				return fmt.Errorf("not enough inventory space to unequip %s", itm.Name)
			}
			equippedRing := data.Position{X: ui.EquipRRinX, Y: ui.EquipRRinY}
			if ctx.Data.LegacyGraphics {
				equippedRing = data.Position{X: ui.EquipRRinClassicX, Y: ui.EquipRRinClassicY}
			}
			ctx.HID.ClickWithModifier(game.LeftButton, equippedRing.X, equippedRing.Y, game.ShiftKey)
			utils.Sleep(EquipDelayMS)

		case item.LocRightArm:
			for _, equippedItem := range ctx.Data.Inventory.ByLocation(item.LocationEquipped) {
				if equippedItem.Location.BodyLocation == item.LocRightArm {
					if !itemFitsInventory(itm) {
						return fmt.Errorf("not enough inventory space to unequip %s", itm.Name)
					}

					equippedRightArm := data.Position{X: ui.EquipRArmX, Y: ui.EquipRArmY}
					if ctx.Data.LegacyGraphics {
						equippedRightArm = data.Position{X: ui.EquipRArmClassicX, Y: ui.EquipRArmClassicY}
					}
					ctx.HID.ClickWithModifier(game.LeftButton, equippedRightArm.X, equippedRightArm.Y, game.ShiftKey)
					utils.Sleep(EquipDelayMS)
					break
				}
			}
		}
		ctx.Logger.Debug(fmt.Sprintf("Equipping %s at %v to %s using hotkeys", itm.Name, itemCoords, bodyloc))
		ctx.HID.ClickWithModifier(game.LeftButton, itemCoords.X, itemCoords.Y, game.ShiftKey)
	}

	utils.Sleep(500)
	*ctx.Data = ctx.GameReader.GetData()

	itemEquipped := false
	for _, inPlace := range ctx.Data.Inventory.ByLocation(target) {
		if itm.UnitID == inPlace.UnitID && inPlace.Location.BodyLocation == bodyloc {
			itemEquipped = true
			break
		}
	}

	if itemEquipped {
		return nil
	} else {
		ctx.Logger.Error(fmt.Sprintf("Failed to equip %s to %s using hotkeys", itm.Name, target))
		return fmt.Errorf("failed to equip %s to %s", itm.Name, target)
	}
}

func FindItemByUnitID(inventory data.Inventory, unitID data.UnitID) data.Item {
	for _, itm := range inventory.AllItems {
		if itm.UnitID == unitID {
			return itm
		}
	}
	return data.Item{} // Return an empty item if not found
}

// GetEquippedItem is a new helper function to search for the currently equipped item in a specific location
func GetEquippedItem(inventory data.Inventory, loc item.LocationType) data.Item {
	for _, itm := range inventory.ByLocation(item.LocationEquipped) {
		if itm.Location.BodyLocation == loc {
			return itm
		}
	}
	return data.Item{}
}

// GetMercEquippedItem is a new helper function for the merc
func GetMercEquippedItem(inventory data.Inventory, loc item.LocationType) data.Item {
	for _, itm := range inventory.ByLocation(item.LocationMercenary) {
		if itm.Location.BodyLocation == loc {
			return itm
		}
	}
	return data.Item{}
}
