package step

import (
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/hectorgimenez/d2go/pkg/data"
	"github.com/hectorgimenez/d2go/pkg/data/object"
	"github.com/hectorgimenez/d2go/pkg/data/skill"
	"github.com/hectorgimenez/d2go/pkg/data/state"
	"github.com/hectorgimenez/koolo/internal/context"
	"github.com/hectorgimenez/koolo/internal/game"
	"github.com/hectorgimenez/koolo/internal/ui"
	"github.com/hectorgimenez/koolo/internal/utils"
)

const DistanceToFinishMoving = 4

var (
	ErrMonstersInPath           = errors.New("monsters detected in movement path")
	stepLastMonsterCheck        = time.Time{}
	stepMonsterCheckInterval    = 100 * time.Millisecond
	lastDestructibleAttemptTime = time.Time{}
	objectInteractionCooldown   = 500 * time.Millisecond
)

var alwaysTakeShrines = []object.ShrineType{
	object.RefillShrine,
	object.HealthShrine,
	object.ManaShrine,
}

var prioritizedShrines = []struct {
	shrineType object.ShrineType
	state      state.State
}{
	{shrineType: object.ExperienceShrine, state: state.ShrineExperience},
	{shrineType: object.ManaRegenShrine, state: state.ShrineManaRegen},
	{shrineType: object.StaminaShrine, state: state.ShrineStamina},
	{shrineType: object.SkillShrine, state: state.ShrineSkill},
}

// New list of shrines to prioritize when a curse is active
var curseBreakingShrines = []object.ShrineType{
	object.ExperienceShrine,
	object.ManaRegenShrine,
	object.StaminaShrine,
	object.SkillShrine,
	object.ArmorShrine,
	object.CombatShrine,
	object.ResistLightningShrine,
	object.ResistFireShrine,
	object.ResistColdShrine,
	object.ResistPoisonShrine,
}

type MoveOpts struct {
	distanceOverride      *int
	stationaryMinDistance *int
	stationaryMaxDistance *int
	ignoreShrines         bool
}

type MoveOption func(*MoveOpts)

func WithDistanceToFinish(distance int) MoveOption {
	return func(opts *MoveOpts) {
		opts.distanceOverride = &distance
	}
}

func WithStationaryDistance(min, max int) MoveOption {
	return func(opts *MoveOpts) {
		opts.stationaryMinDistance = &min
		opts.stationaryMaxDistance = &max
	}
}

func IgnoreShrines() MoveOption {
	return func(opts *MoveOpts) {
		opts.ignoreShrines = true
	}
}

func calculateDistance(p1, p2 data.Position) float64 {
	dx := float64(p1.X - p2.X)
	dy := float64(p1.Y - p2.Y)
	return math.Sqrt(dx*dx + dy*dy)
}

func MoveTo(dest data.Position, options ...MoveOption) error {
	opts := &MoveOpts{}
	for _, o := range options {
		o(opts)
	}

	minDistanceToFinishMoving := DistanceToFinishMoving
	if opts.distanceOverride != nil {
		minDistanceToFinishMoving = *opts.distanceOverride
	}

	ctx := context.Get()
	ctx.SetLastStep("MoveTo")

	timeout := time.Second * 30
	idleThreshold := time.Second * 3
	stuckThreshold := 150 * time.Millisecond

	idleStartTime := time.Time{}
	stuckCheckStartTime := time.Time{}
	openedDoors := make(map[object.Name]data.Position)

	var walkDuration time.Duration
	if !ctx.Data.AreaData.Area.IsTown() {
		walkDuration = utils.RandomDurationMs(300, 350)
	} else {
		walkDuration = utils.RandomDurationMs(500, 800)
	}

	startedAt := time.Now()
	lastRun := time.Time{}
	previousPosition := data.Position{}

	longTermIdleReferencePosition := data.Position{}
	longTermIdleStartTime := time.Time{}
	const longTermIdleThreshold = 2 * time.Minute
	const minMovementThreshold = 30

	stuckCounter := 0
	const increasingSleepBase = 50 * time.Millisecond
	const minMovementToResetStuckCounter = 10

	stuckLoopAttempts := 0

	// Map for long-term failures (15s cooldown)
	failedToDestroyLongTerm := make(map[object.Name]time.Time)
	// Map for short-term failures (3s cooldown)
	failedToDestroyShortTerm := make(map[object.Name]time.Time)
	// New map to track consecutive destruction failures for each object
	failedAttemptsCount := make(map[object.Name]int)

	failedToPathToShrine := make(map[data.Position]time.Time)

	var shrineDestination data.Position
	var doorDestination data.Position
	var doorObject *data.Object

	// New constant for destructible object ignore duration
	const destructibleIgnoreDuration = 3 * time.Second

	// New variables for Shrine-Stuck logic
	var shrineStuckStartTime time.Time
	var shrineStuckReferencePosition data.Position
	const shrineStuckThreshold = 2 * time.Second
	const shrineMovementThreshold = 5.0

	// New variables for Monster-Stuck logic
	var monsterStuckStartTime time.Time
	var monsterStuckLastKnownPosition data.Position
	const monsterStuckThreshold = 20 * time.Second
	const minMovementToResetMonsterStuck = 10.0

	for {
		ctx.PauseIfNotPriority()
		ctx.RefreshGameData()
		currentDest := dest

		if !opts.ignoreShrines && shrineDestination == (data.Position{}) && !ctx.Data.AreaData.Area.IsTown() {
			if closestShrine := findClosestShrine(); closestShrine != nil {
				if failedTime, exists := failedToPathToShrine[closestShrine.Position]; exists {
					if time.Since(failedTime) < 5*time.Minute {
						ctx.Logger.Debug("Skipping shrine as it was previously unreachable and is on cooldown.")
						shrineDestination = data.Position{}
						currentDest = dest
					} else {
						delete(failedToPathToShrine, closestShrine.Position)
						shrineDestination = closestShrine.Position
						ctx.Logger.Debug(fmt.Sprintf("MoveTo: Found shrine at %v, redirecting destination from %v", closestShrine.Position, dest))
					}
				} else {
					shrineDestination = closestShrine.Position
					ctx.Logger.Debug(fmt.Sprintf("MoveTo: Found shrine at %v, redirecting destination from %v", closestShrine.Position, dest))
				}
			}
		}

		if doorDestination != (data.Position{}) {
			currentDest = doorDestination
		} else if shrineDestination != (data.Position{}) {
			currentDest = shrineDestination
		} else {
			currentDest = dest
		}

		if !ctx.Data.CanTeleport() {
			obstaclePos, obj, err := handleObstaclesInPath(currentDest, openedDoors, failedToDestroyLongTerm, failedToDestroyShortTerm, failedAttemptsCount, destructibleIgnoreDuration)
			if err != nil {
				return err
			}
			if obstaclePos != (data.Position{}) {
				doorDestination = obstaclePos
				doorObject = obj
			}
		}

		// --- MONSTER CHECK LOGIC ---
		if time.Since(stepLastMonsterCheck) > stepMonsterCheckInterval {
			if doorDestination == (data.Position{}) {
				closestMonster, found := findClosestMonsterInPath(currentDest)
				stepLastMonsterCheck = time.Now()

				if found {
					// Bot sees a monster and wants to engage. This is the normal behavior.
					if monsterStuckStartTime.IsZero() {
						monsterStuckStartTime = time.Now()
						monsterStuckLastKnownPosition = closestMonster.Position
					}
					ctx.Logger.Debug("Monsters detected within safe zone for non-teleporter. Engaging enemies before attempting movement.")
					return ErrMonstersInPath
				} else {
					// No monster found, reset the monster-stuck timer.
					monsterStuckStartTime = time.Time{}
				}
			}
		}

		// --- NEW MONSTER-STUCK FALLBACK LOGIC ---
		// If we've been trying to engage monsters for too long without success
		if !monsterStuckStartTime.IsZero() && time.Since(monsterStuckStartTime) > monsterStuckThreshold {
			ctx.Logger.Warn(fmt.Sprintf("Bot seems to be stuck in a monster-clearing loop for more than %v. Attempting to move towards monster's last known position as a fallback.", monsterStuckThreshold))

			// Calculate a new destination 5 units away from the monster
			currentPos := ctx.Data.PlayerUnit.Position
			dx := float64(monsterStuckLastKnownPosition.X - currentPos.X)
			dy := float64(monsterStuckLastKnownPosition.Y - currentPos.Y)
			dist := math.Sqrt(dx*dx + dy*dy)

			// New destination is 5 units away from the monster in a straight line.
			var newDest data.Position
			if dist > 5 {
				newDest = data.Position{
					X: int(float64(monsterStuckLastKnownPosition.X) - (dx/dist)*5),
					Y: int(float64(monsterStuckLastKnownPosition.Y) - (dy/dist)*5),
				}
			} else {
				// If we are already too close, just move to the monster's position.
				newDest = monsterStuckLastKnownPosition
			}

			// Log the movement and perform a recursive call with a distance override.
			ctx.Logger.Debug(fmt.Sprintf("Forcing a movement to position %v, which is 5 units away from the monster's last known location %v", newDest, monsterStuckLastKnownPosition))
			if err := MoveTo(newDest, WithDistanceToFinish(5)); err != nil {
				ctx.Logger.Error("Failed to perform forced movement to break monster-stuck loop.", slog.Any("error", err))
			}

			// Reset the stuck timer to prevent an immediate re-trigger.
			monsterStuckStartTime = time.Time{}

			// Continue the loop to re-evaluate the situation after the movement.
			continue
		}
		// --- END MONSTER-STUCK FALLBACK LOGIC ---

		currentDistanceToDest := float64(ctx.PathFinder.DistanceFromMe(currentDest))

		if opts.stationaryMinDistance != nil && opts.stationaryMaxDistance != nil {
			if currentDistanceToDest >= float64(*opts.stationaryMinDistance) && currentDistanceToDest <= float64(*opts.stationaryMaxDistance) {
				ctx.Logger.Debug(fmt.Sprintf("MoveTo: Reached stationary distance %d-%d (current %.2f)", *opts.stationaryMinDistance, *opts.stationaryMaxDistance, currentDistanceToDest))
				return nil
			}
		}

		if currentDistanceToDest < float64(minDistanceToFinishMoving) {
			if doorDestination != (data.Position{}) {
				if doorObject != nil {
					ctx.Logger.Debug("Reached door, attempting to open it.")

					err := InteractObject(*doorObject, func() bool {
						obj, found := ctx.Data.Objects.FindByID(doorObject.ID)
						return found && !obj.Selectable
					})

					if err != nil {
						ctx.Logger.Warn("Failed to open door", slog.Any("error", err))
					} else {
						ctx.Logger.Debug("Door successfully opened.")
					}
				}
				doorDestination = data.Position{}
				doorObject = nil
				if doorObject != nil {
					openedDoors[doorObject.Name] = doorObject.Position
				}
				continue
			}

			if shrineDestination != (data.Position{}) && shrineDestination == currentDest {
				shrineFound := false
				var shrineObject data.Object
				for _, o := range ctx.Data.Objects {
					if o.Position == shrineDestination {
						shrineObject = o
						shrineFound = true
						break
					}
				}

				if shrineFound {
					if err := interactWithShrine(&shrineObject); err != nil {
						ctx.Logger.Warn("Failed to interact with shrine", slog.Any("error", err))
					}
				}

				shrineDestination = data.Position{}
				continue
			}

			if currentDest == dest {
				return nil
			}
		}

		// --- SHRINE-STUCK LOGIC ---
		if currentDest == shrineDestination {
			currentPosition := ctx.Data.PlayerUnit.Position
			if shrineStuckStartTime.IsZero() {
				shrineStuckStartTime = time.Now()
				shrineStuckReferencePosition = currentPosition
			} else {
				distanceMoved := calculateDistance(currentPosition, shrineStuckReferencePosition)
				if distanceMoved > shrineMovementThreshold {
					shrineStuckStartTime = time.Time{}
				} else if time.Since(shrineStuckStartTime) > shrineStuckThreshold {
					ctx.Logger.Warn(fmt.Sprintf("Bot appears to be stuck at shrine destination for %v. Forcing interaction.", shrineStuckThreshold))
					shrineFound := false
					var shrineObject data.Object
					for _, o := range ctx.Data.Objects {
						if o.Position == shrineDestination {
							shrineObject = o
							shrineFound = true
							break
						}
					}
					if shrineFound {
						if err := interactWithShrine(&shrineObject); err != nil {
							ctx.Logger.Warn("Failed to interact with shrine after being stuck", slog.Any("error", err))
						}
					} else {
						ctx.Logger.Warn("Stuck at a location that was supposed to be a shrine, but no shrine object was found. Clearing shrine destination.")
					}
					shrineDestination = data.Position{}
					shrineStuckStartTime = time.Time{}
					continue
				}
			}
		} else {
			shrineStuckStartTime = time.Time{}
		}

		if !ctx.Data.CanTeleport() {
			if time.Since(lastRun) < walkDuration {
				time.Sleep(walkDuration - time.Since(lastRun))
				continue
			}
		} else {
			if time.Since(lastRun) < ctx.Data.PlayerCastDuration() {
				time.Sleep(ctx.Data.PlayerCastDuration() - time.Since(lastRun))
				continue
			}
		}

		currentPosition := ctx.Data.PlayerUnit.Position

		// Reset monster stuck timer if the bot moves significantly.
		distanceFromLastMonsterCheck := calculateDistance(currentPosition, previousPosition)
		if distanceFromLastMonsterCheck > minMovementToResetMonsterStuck {
			monsterStuckStartTime = time.Time{}
		}

		if longTermIdleStartTime.IsZero() {
			longTermIdleReferencePosition = currentPosition
			longTermIdleStartTime = time.Now()
		}

		distanceFromLongTermReference := calculateDistance(longTermIdleReferencePosition, currentPosition)

		if distanceFromLongTermReference > float64(minMovementThreshold) {
			longTermIdleStartTime = time.Time{}
			ctx.Logger.Debug(fmt.Sprintf("MoveTo: Player moved significantly (%.2f units), resetting long-term idle timer.", distanceFromLongTermReference))
		} else if time.Since(longTermIdleStartTime) > longTermIdleThreshold {
			ctx.Logger.Error(fmt.Sprintf("MoveTo: Player has been idle for more than %v, quitting game.", longTermIdleThreshold))
			return errors.New("player idle for too long, quitting game")
		}

		if currentPosition == previousPosition {
			if stuckCheckStartTime.IsZero() {
				stuckCheckStartTime = time.Now()
			} else if time.Since(stuckCheckStartTime) > stuckThreshold {
				ctx.Logger.Debug("Bot stuck (short term), attempting micro-shuffle.")
				ctx.PathFinder.RandomMovement()
				stuckCheckStartTime = time.Time{}
				idleStartTime = time.Time{}

				stuckCounter++
				if stuckCounter > 100 {
					stuckLoopAttempts++
					switch stuckLoopAttempts {
					case 1:
						minDistanceToFinishMoving = 20
						ctx.Logger.Warn(fmt.Sprintf("Bot seems to be stuck, reducing distance to finish to %d", minDistanceToFinishMoving))
					case 2:
						minDistanceToFinishMoving = 10
						ctx.Logger.Warn(fmt.Sprintf("Bot is still stuck, further reducing distance to finish to %d", minDistanceToFinishMoving))
					case 3:
						minDistanceToFinishMoving = 5
						ctx.Logger.Warn(fmt.Sprintf("Bot is still stuck, final attempt with distance to finish to %d", minDistanceToFinishMoving))
					default:
						return errors.New("player stuck in a movement loop, quitting")
					}
					stuckCounter = 0
				}

				sleepDuration := increasingSleepBase * time.Duration(stuckCounter)
				ctx.Logger.Debug(fmt.Sprintf("Sleeping for %v before next loop.", sleepDuration))
				time.Sleep(sleepDuration)

			} else if idleStartTime.IsZero() {
				idleStartTime = time.Now()
			} else if time.Since(idleStartTime) > idleThreshold {
				ctx.Logger.Debug("Bot stuck (long term / idle), performing random movement as fallback.")
				ctx.PathFinder.RandomMovement()
				idleStartTime = time.Time{}
				stuckCheckStartTime = time.Time{}
			}
		} else {
			distanceMoved := calculateDistance(currentPosition, previousPosition)

			if distanceMoved > float64(minMovementToResetStuckCounter) {
				idleStartTime = time.Time{}
				stuckCheckStartTime = time.Time{}
				stuckCounter = 0
				stuckLoopAttempts = 0
			}
			previousPosition = currentPosition
		}

		if ctx.Data.CanTeleport() {
			if ctx.Data.PlayerUnit.RightSkill != skill.Teleport {
				ctx.HID.PressKeyBinding(ctx.Data.KeyBindings.MustKBForSkill(skill.Teleport))
			}
		} else if kb, found := ctx.Data.KeyBindings.KeyBindingForSkill(skill.Vigor); found {
			if ctx.Data.PlayerUnit.RightSkill != skill.Vigor {
				ctx.HID.PressKeyBinding(kb)
			}
		}

		path, distance, found := ctx.PathFinder.GetPath(currentDest)
		if !found {
			if currentDest == shrineDestination {
				ctx.Logger.Warn(fmt.Sprintf("Path to shrine at %v could not be calculated. Marking shrine as unreachable for a few minutes.", currentDest))
				failedToPathToShrine[shrineDestination] = time.Now()
				shrineDestination = data.Position{}
				continue
			}
			if currentDest == doorDestination {
				ctx.Logger.Warn(fmt.Sprintf("Path to door at %v could not be calculated. Clearing door destination.", currentDest))
				doorDestination = data.Position{}
				doorObject = nil
				continue
			}

			if opts.stationaryMinDistance == nil || opts.stationaryMaxDistance == nil ||
				currentDistanceToDest < float64(*opts.stationaryMinDistance) || currentDistanceToDest > float64(*opts.stationaryMaxDistance) {
				if float64(ctx.PathFinder.DistanceFromMe(currentDest)) < float64(minDistanceToFinishMoving+5) {
					return nil
				}
				continue
			}
			return nil
		}

		if float64(distance) <= float64(minDistanceToFinishMoving) || len(path) <= minDistanceToFinishMoving || len(path) == 0 {
			return nil
		}

		if timeout > 0 && time.Since(startedAt) > timeout {
			return nil
		}

		lastRun = time.Now()

		previousPosition = ctx.Data.PlayerUnit.Position
		ctx.PathFinder.MoveThroughPath(path, walkDuration)
	}
}

// handleObstaclesInPath manages interactions with doors and destructible objects in the bot's path.
// It now includes a global cooldown on interaction attempts and a "give up" threshold for a specific object.
func handleObstaclesInPath(dest data.Position, openedDoors map[object.Name]data.Position, failedToDestroyLongTerm map[object.Name]time.Time, failedToDestroyShortTerm map[object.Name]time.Time, failedAttemptsCount map[object.Name]int, destructibleIgnoreDuration time.Duration) (data.Position, *data.Object, error) {
	ctx := context.Get()

	const immediateVicinity = 5.0
	for _, o := range ctx.Data.Objects {
		if o.IsDoor() && o.Selectable {
			ourPos := ctx.Data.PlayerUnit.Position
			distanceToDoor := calculateDistance(ourPos, o.Position)
			if distanceToDoor < immediateVicinity {
				ctx.Logger.Debug("Door detected in immediate vicinity, prioritizing opening it.")
				return o.Position, &o, nil
			}
		}
	}

	var closestDoorInPath *data.Object
	minDistance := math.MaxFloat64

	for _, o := range ctx.Data.Objects {
		if o.IsDoor() && o.Selectable {
			doorPos := o.Position
			ourPos := ctx.Data.PlayerUnit.Position
			threshhold := 5.0
			if isObjectInPath(dest, ourPos, doorPos, threshhold) {
				distance := float64(ctx.PathFinder.DistanceFromMe(doorPos))
				if distance < minDistance {
					minDistance = distance
					closestDoorInPath = &o
				}
			}
		}
	}

	if closestDoorInPath != nil {
		ctx.Logger.Debug("Door detected in path, setting as new destination.")
		return closestDoorInPath.Position, closestDoorInPath, nil
	}

	breakableObjects := []object.Name{
		object.Barrel, object.Urn2, object.Urn3, object.Casket,
		object.Casket5, object.Casket6, object.LargeUrn1, object.LargeUrn4,
		object.LargeUrn5, object.Crate, object.HollowLog, object.Sarcophagus,
	}

	const longTermFailureCooldown = 5 * time.Minute
	const consecutiveFailureThreshold = 3

	// Global cooldown check to avoid spamming interaction attempts
	if time.Since(lastDestructibleAttemptTime) < objectInteractionCooldown {
		return data.Position{}, nil, nil
	}

	for _, o := range ctx.Data.Objects {
		for _, breakableName := range breakableObjects {
			if o.Name == breakableName && o.Selectable && ctx.PathFinder.DistanceFromMe(o.Position) < 3 {

				if failedTime, exists := failedToDestroyLongTerm[o.Name]; exists {
					if time.Since(failedTime) < longTermFailureCooldown {
						ctx.Logger.Debug(fmt.Sprintf("Skipping object [%s] due to active long-term cooldown.", o.Desc().Name))
						continue
					} else {
						delete(failedToDestroyLongTerm, o.Name)
					}
				}

				if failedTime, exists := failedToDestroyShortTerm[o.Name]; exists {
					if time.Since(failedTime) < destructibleIgnoreDuration {
						ctx.Logger.Debug(fmt.Sprintf("Destructible object [%s] detected, but within the %v ignore window. Attempting to proceed with movement.", o.Desc().Name, destructibleIgnoreDuration))
						continue
					} else {
						delete(failedToDestroyShortTerm, o.Name)
					}
				}

				objPos := o.Position
				ourPos := ctx.Data.PlayerUnit.Position
				distanceToObj := ctx.PathFinder.DistanceFromMe(objPos)

				dotProduct := (objPos.X-ourPos.X)*(dest.X-ourPos.X) + (objPos.Y-ourPos.Y)*(dest.Y-ourPos.Y)
				lengthSquared := (dest.X-ourPos.X)*(dest.X-ourPos.X) + (dest.Y-ourPos.Y)*(dest.Y-ourPos.Y)

				monsterInPath, found := findClosestMonsterInPath(o.Position)
				if found {
					ctx.Logger.Debug(fmt.Sprintf("Monster [%s] detected on path to destructible object, will not engage object.", monsterInPath.Name))
					return data.Position{}, nil, nil
				}

				if distanceToObj < 10 && lengthSquared > 0 && dotProduct > 0 && dotProduct < lengthSquared {
					ctx.Logger.Debug(fmt.Sprintf("Destructible object [%s] in path, destroying it with LeftClick...", o.Desc().Name))

					// Update the global cooldown time before making the attempt
					lastDestructibleAttemptTime = time.Now()

					err := MoveTo(o.Position, IgnoreShrines())
					if err != nil {
						ctx.Logger.Warn("Failed to move to object", slog.Any("error", err))
						failedToDestroyLongTerm[o.Name] = time.Now()
						failedToDestroyShortTerm[o.Name] = time.Now()
						return data.Position{}, nil, nil
					}

					attempts := 0
					maxAttempts := 5

					for {
						ctx.RefreshGameData()
						_, found := ctx.Data.Objects.FindByID(o.ID)

						if !found {
							ctx.Logger.Debug("Object successfully destroyed. Resetting failure counter.")
							delete(failedAttemptsCount, o.Name) // Reset counter on success
							return data.Position{}, nil, nil
						}

						if attempts >= maxAttempts {
							// Increment the consecutive failure counter
							failedAttemptsCount[o.Name]++
							if failedAttemptsCount[o.Name] >= consecutiveFailureThreshold {
								ctx.Logger.Error(fmt.Sprintf("Failed to destroy object [%s] after multiple attempts (%d times). Giving up on this object for a long time.", o.Desc().Name, failedAttemptsCount[o.Name]))
								failedToDestroyLongTerm[o.Name] = time.Now()
								delete(failedAttemptsCount, o.Name) // Reset counter after giving up
							} else {
								ctx.Logger.Warn(fmt.Sprintf("Failed to destroy object [%s] after multiple attempts. Will retry later.", o.Desc().Name))
							}
							failedToDestroyShortTerm[o.Name] = time.Now()
							return data.Position{}, nil, nil
						}

						x, y := ui.GameCoordsToScreenCords(o.Position.X, o.Position.Y)
						ctx.HID.Click(game.LeftButton, x, y)
						attempts++
						utils.Sleep(100)
					}
				}
			}
		}
	}
	return data.Position{}, nil, nil
}

func findClosestMonsterInPath(dest data.Position) (*data.Monster, bool) {
	ctx := context.Get()

	isGoingToPortal := false
	for _, o := range ctx.Data.Objects {
		if o.Name == object.TownPortal && o.Position == dest {
			isGoingToPortal = true
			break
		}
	}
	if ctx.Data.PlayerUnit.Area.IsTown() || isGoingToPortal {
		return nil, false
	}

	hostileMonsters := ctx.Data.Monsters.Enemies(data.MonsterAnyFilter())

	var closestMonster *data.Monster
	var minDistance float64 = math.MaxFloat64

	for _, m := range hostileMonsters {
		monsterDistance := float64(ctx.PathFinder.DistanceFromMe(m.Position))
		if monsterDistance < float64(ctx.CharacterCfg.Character.ClearPathDist) && !ctx.Data.CanTeleport() {
			if ctx.PathFinder.LineOfSight(ctx.Data.PlayerUnit.Position, m.Position) {
				if monsterDistance < minDistance {
					minDistance = monsterDistance
					closestMonster = &m
				}
			} else {
				ctx.Logger.Debug(fmt.Sprintf("Monster [%v] is nearby but no line of sight, continuing movement.", m.Name))
			}
		}
	}

	if closestMonster != nil {
		return closestMonster, true
	}

	return nil, false
}

func findClosestShrine() *data.Object {
	ctx := context.Get()

	// --- NEW CURSE-BREAKING LOGIC ---
	// Check if the player has any of the defined curses.
	if ctx.Data.PlayerUnit.States.HasState(state.Amplifydamage) ||
		ctx.Data.PlayerUnit.States.HasState(state.Lowerresist) ||
		ctx.Data.PlayerUnit.States.HasState(state.Decrepify) {

		ctx.Logger.Debug("Curse detected on player. Prioritizing finding any shrine to break it.")

		var closestCurseBreakingShrine *data.Object
		minDistance := math.MaxFloat64

		for _, o := range ctx.Data.Objects {
			// Find any shrine that is selectable
			if o.IsShrine() && o.Selectable {
				for _, sType := range curseBreakingShrines {
					if o.Shrine.ShrineType == sType {
						distance := float64(ctx.PathFinder.DistanceFromMe(o.Position))
						if distance < minDistance {
							minDistance = distance
							closestCurseBreakingShrine = &o
						}
					}
				}
			}
		}
		if closestCurseBreakingShrine != nil {
			return closestCurseBreakingShrine
		}
	}
	// --- END NEW CURSE-BREAKING LOGIC ---

	var closestShrine *data.Object
	var minDistance float64 = math.MaxFloat64

	for _, o := range ctx.Data.Objects {
		// Only consider shrines that are selectable (not already used)
		if o.IsShrine() && o.Selectable {
			takeShrine := false
			// Check for Health, Mana, and Refill shrines with conditional logic
			switch o.Shrine.ShrineType {
			case object.HealthShrine:
				// Take Health Shrine if HP is below 80%
				if ctx.Data.PlayerUnit.HPPercent() < 80 {
					takeShrine = true
				}
			case object.ManaShrine:
				// Take Mana Shrine if MP is below 50%
				if ctx.Data.PlayerUnit.MPPercent() < 50 {
					takeShrine = true
				}
			case object.RefillShrine:
				// Take Refill Shrine if either HP is below 80% or MP is below 50%
				if ctx.Data.PlayerUnit.HPPercent() < 80 || ctx.Data.PlayerUnit.MPPercent() < 50 {
					takeShrine = true
				}
			}

			if takeShrine {
				distance := float64(ctx.PathFinder.DistanceFromMe(o.Position))
				if distance < minDistance {
					minDistance = distance
					closestShrine = &o
				}
			}
		}
	}

	if closestShrine != nil {
		return closestShrine
	}

	// The original logic for prioritized shrines (like XP, Skill) remains the same.
	var currentPriorityIndex int = -1

	for i, p := range prioritizedShrines {
		if ctx.Data.PlayerUnit.States.HasState(p.state) {
			currentPriorityIndex = i
			break
		}
	}

	for _, o := range ctx.Data.Objects {
		if o.IsShrine() && o.Selectable {
			shrinePriorityIndex := -1
			for i, p := range prioritizedShrines {
				if o.Shrine.ShrineType == p.shrineType {
					shrinePriorityIndex = i
					break
				}
			}

			if shrinePriorityIndex != -1 && (currentPriorityIndex == -1 || shrinePriorityIndex <= currentPriorityIndex) {
				distance := float64(ctx.PathFinder.DistanceFromMe(o.Position))
				if distance < minDistance {
					minDistance = distance
					closestShrine = &o
				}
			}
		}
	}

	return closestShrine
}

func interactWithShrine(shrine *data.Object) error {
	ctx := context.Get()
	ctx.Logger.Debug(fmt.Sprintf("Shrine [%s] found. Interacting with it...", shrine.Desc().Name))

	attempts := 0
	maxAttempts := 3

	for {
		ctx.RefreshGameData()
		s, found := ctx.Data.Objects.FindByID(shrine.ID)

		if !found || !s.Selectable {
			ctx.Logger.Debug("Shrine successfully activated.")
			return nil
		}

		if attempts >= maxAttempts {
			ctx.Logger.Warn(fmt.Sprintf("Failed to activate shrine [%s] after multiple attempts. Moving on.", shrine.Desc().Name))
			return fmt.Errorf("failed to activate shrine [%s] after multiple attempts", shrine.Desc().Name)
		}

		x, y := ui.GameCoordsToScreenCords(s.Position.X, s.Position.Y)
		ctx.HID.Click(game.LeftButton, x, y)
		attempts++
		utils.Sleep(100)
	}
}

func isObjectInPath(dest data.Position, player data.Position, object data.Position, tolerance float64) bool {
	ctx := context.Get()
	minX := math.Min(float64(player.X), float64(dest.X)) - tolerance
	maxX := math.Max(float64(player.X), float64(dest.X)) + tolerance
	minY := math.Min(float64(player.Y), float64(dest.Y)) - tolerance
	maxY := math.Max(float64(player.Y), float64(dest.Y)) + tolerance

	if object.X >= int(maxX) || object.X <= int(minX) || object.Y <= int(minY) || object.Y >= int(maxY) {
		return false
	}

	if player.X == dest.X {
		if math.Abs(float64(player.X)-float64(object.X)) >= tolerance {
			ctx.Logger.Debug(fmt.Sprintf("Object is vertical, check failed with value %.2f ", math.Abs(float64(player.X)-float64(object.X))))
			return false
		}
		return true
	}

	if player.Y == dest.Y {
		if math.Abs(float64(player.Y)-float64(object.Y)) >= tolerance {
			ctx.Logger.Debug(fmt.Sprintf("Object is horizontal, check failed with value %.2f ", math.Abs(float64(player.Y)-float64(object.Y))))
			return false
		}
		return true
	}

	distFromLine := math.Abs(((float64(dest.X)-float64(player.X))*(float64(player.Y)-float64(object.Y)))-((float64(player.X)-float64(object.X))*(float64(dest.Y)-float64(player.Y)))) / math.Sqrt((float64(dest.X)-float64(player.X))*(float64(dest.X)-float64(player.X))+(float64(dest.Y)-float64(player.Y))*(float64(dest.Y)-float64(player.Y)))
	ctx.Logger.Debug(fmt.Sprintf("Object is distance: %.2f from our path", distFromLine))
	if distFromLine >= tolerance {
		return false
	} else {
		return true
	}
}
