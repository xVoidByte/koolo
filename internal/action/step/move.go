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
	distanceOverride       *int
	stationaryMinDistance  *int
	stationaryMaxDistance  *int
	ignoreShrines         bool
}

type MoveOption func(*MoveOpts)

// WithDistanceToFinish overrides the default DistanceToFinishMoving
func WithDistanceToFinish(distance int) MoveOption {
	return func(opts *MoveOpts) {
		opts.distanceOverride = &distance
	}
}

// WithStationaryDistance configures MoveTo to stop when within a specific range of the destination.
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

// calculateDistance returns the Euclidean distance between two positions.
func calculateDistance(p1, p2 data.Position) float64 {
	dx := float64(p1.X - p2.X)
	dy := float64(p1.Y - p2.Y)
	return math.Sqrt(dx*dx + dy*dy)
}

func MoveTo(dest data.Position, options ...MoveOption) error {
	// Initialize options
	opts := &MoveOpts{}

	// Apply any provided options
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
	previousDistance := 0

	longTermIdleReferencePosition := data.Position{}
	longTermIdleStartTime := time.Time{}
	const longTermIdleThreshold = 2 * time.Minute
	const minMovementThreshold = 30
	failedToPathToShrine := make(map[data.Position]time.Time)
	var shrineDestination data.Position
	var obstacleDestination data.Position
	var obstacleObject *data.Object
	
	// New variables for Shrine-Stuck logic
	var shrineStuckStartTime time.Time
	var shrineStuckReferencePosition data.Position
	const shrineStuckThreshold = 2 * time.Second
	const shrineMovementThreshold = 5.0

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

		// Handle obstacles. Prioritize immediate obstacles, then obstacles in path.
		if !ctx.Data.CanTeleport() {
			// Prioritize objects in the immediate vicinity first
			if objectToInteract, found := handleImmediateObstacles(); found {
				obstacleDestination = objectToInteract.Position
				obstacleObject = objectToInteract
			} else {
				// Then, check for obstacles directly in the path
				obstaclePos, obj, err := handleDoorsInPath(currentDest, openedDoors)
				if err != nil {
					return err
				}
				if obstaclePos != (data.Position{}) {
					obstacleDestination = obstaclePos
					obstacleObject = obj
				}
			}
		}
		
		if obstacleDestination != (data.Position{}) {
			currentDest = obstacleDestination
		} else if shrineDestination != (data.Position{}) {
			currentDest = shrineDestination
		} else {
			currentDest = dest
		}

		// --- MONSTER CHECK LOGIC ---
		if time.Since(stepLastMonsterCheck) > stepMonsterCheckInterval {
			if obstacleDestination == (data.Position{}) {
				_, found := findClosestMonsterInPath(currentDest)
				stepLastMonsterCheck = time.Now()

				if found {
					ctx.Logger.Debug("Monsters detected within safe zone for non-teleporter. Engaging enemies before attempting movement.")
					return ErrMonstersInPath
				}
			}
		}

		currentDistanceToDest := float64(ctx.PathFinder.DistanceFromMe(currentDest))
		if opts.stationaryMinDistance != nil && opts.stationaryMaxDistance != nil {
			if currentDistanceToDest >= float64(*opts.stationaryMinDistance) && currentDistanceToDest <= float64(*opts.stationaryMaxDistance) {
				ctx.Logger.Debug(fmt.Sprintf("MoveTo: Reached stationary distance %d-%d (current %.2f)", *opts.stationaryMinDistance, *opts.stationaryMaxDistance, currentDistanceToDest))
				return nil
			}
		}
		
		if currentDistanceToDest < float64(minDistanceToFinishMoving) {
			if obstacleDestination != (data.Position{}) {
				if obstacleObject != nil {
					// Check for breakable object first
					if isBreakable(obstacleObject.Name) {
						err := InteractWithObject(*obstacleObject)
						if err != nil {
							ctx.Logger.Warn("Failed to destroy breakable object", slog.Any("error", err))
						} else {
							ctx.Logger.Debug("Breakable object successfully destroyed.")
						}
					} else if obstacleObject.IsDoor() { // Then check for doors
						ctx.Logger.Debug("Reached door, attempting to open it.")
						err := InteractObject(*obstacleObject, func() bool {
							obj, found := ctx.Data.Objects.FindByID(obstacleObject.ID)
							return found && !obj.Selectable
						})

						if err != nil {
							ctx.Logger.Warn("Failed to open door", slog.Any("error", err))
						} else {
							ctx.Logger.Debug("Door successfully opened.")
						}
					}
				}
				obstacleDestination = data.Position{}
				obstacleObject = nil
				if obstacleObject != nil {
					openedDoors[obstacleObject.Name] = obstacleObject.Position
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
			} else if idleStartTime.IsZero() {
				idleStartTime = time.Now()
			} else if time.Since(idleStartTime) > idleThreshold {
				ctx.Logger.Debug("Bot stuck (long term / idle), performing random movement as fallback.")
				ctx.PathFinder.RandomMovement()
				idleStartTime = time.Time{}
				stuckCheckStartTime = time.Time{}
			}
		} else {
			idleStartTime = time.Time{}
			stuckCheckStartTime = time.Time{}
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
			if currentDest == obstacleDestination {
				ctx.Logger.Warn(fmt.Sprintf("Path to obstacle at %v could not be calculated. Clearing obstacle destination.", currentDest))
				obstacleDestination = data.Position{}
				obstacleObject = nil
				continue
			}
			if opts.stationaryMinDistance == nil || opts.stationaryMaxDistance == nil ||
				currentDistanceToDest < float64(*opts.stationaryMinDistance) || currentDistanceToDest > float64(*opts.stationaryMaxDistance) {
				if float64(ctx.PathFinder.DistanceFromMe(currentDest)) < float64(minDistanceToFinishMoving+5) {
					return nil
				}
				return errors.New("path could not be calculated. Current area: [" + ctx.Data.PlayerUnit.Area.Area().Name + "]. Trying to path to Destination: [" + fmt.Sprintf("%d,%d", currentDest.X, currentDest.Y) + "]")
			}
			return nil
		}
		if float64(distance) <= float64(minDistanceToFinishMoving) || len(path) <= minDistanceToFinishMoving || len(path) == 0 {
			if currentDest == dest {
				return nil
			}
			if currentDest == shrineDestination {
				shrineDestination = data.Position{}
				continue
			}
		}

		if timeout > 0 && time.Since(startedAt) > timeout {
			return nil
		}

		lastRun = time.Now()

		if distance < 20 && math.Abs(float64(previousDistance-distance)) < float64(DistanceToFinishMoving) {
			minDistanceToFinishMoving += DistanceToFinishMoving
		} else if opts.distanceOverride != nil {
			minDistanceToFinishMoving = *opts.distanceOverride
		} else {
			minDistanceToFinishMoving = DistanceToFinishMoving
		}

		previousPosition = ctx.Data.PlayerUnit.Position
		previousDistance = distance
		ctx.PathFinder.MoveThroughPath(path, walkDuration)
	}
}

// handleImmediateObstacles checks for doors or breakable objects in the immediate vicinity and returns the closest one.
func handleImmediateObstacles() (*data.Object, bool) {
	ctx := context.Get()
	breakableObjects := []object.Name{
		object.Barrel, object.Urn2, object.Urn3, object.Casket,
		object.Casket5, object.Casket6, object.LargeUrn1, object.LargeUrn4,
		object.LargeUrn5, object.Crate, object.HollowLog, object.Sarcophagus,
	}

	const immediateVicinity = 5.0
	var closestObject *data.Object
	minDistance := math.MaxFloat64

	// First, check for breakable objects
	for _, o := range ctx.Data.Objects {
		for _, breakableName := range breakableObjects {
			if o.Name == breakableName && o.Selectable {
				ourPos := ctx.Data.PlayerUnit.Position
				distanceToObj := calculateDistance(ourPos, o.Position)
				if distanceToObj < immediateVicinity && distanceToObj < minDistance {
					minDistance = distanceToObj
					closestObject = &o
				}
			}
		}
	}

	// Then, check for doors. If a closer door is found, prioritize it.
	for _, o := range ctx.Data.Objects {
		if o.IsDoor() && o.Selectable {
			ourPos := ctx.Data.PlayerUnit.Position
			distanceToDoor := calculateDistance(ourPos, o.Position)
			if distanceToDoor < immediateVicinity && distanceToDoor < minDistance {
				minDistance = distanceToDoor
				closestObject = &o
			}
		}
	}

	if closestObject != nil {
		ctx.Logger.Debug(fmt.Sprintf("Immediate obstacle found: %s at %s. Prioritizing it.", closestObject.Desc().Name, closestObject.Position))
		return closestObject, true
	}

	return nil, false
}

// InteractWithObject handles the interaction logic for a given object (e.g., a breakable urn)
func InteractWithObject(o data.Object) error {
	ctx := context.Get()

	// Global cooldown check to avoid spamming interaction attempts
	if time.Since(lastDestructibleAttemptTime) < objectInteractionCooldown {
		return nil // Return gracefully if on cooldown
	}

	lastDestructibleAttemptTime = time.Now()
	
	attempts := 0
	maxAttempts := 5

	for {
		ctx.RefreshGameData()
		_, found := ctx.Data.Objects.FindByID(o.ID)

		if !found {
			ctx.Logger.Debug("Object successfully destroyed.")
			return nil
		}

		if attempts >= maxAttempts {
			ctx.Logger.Warn(fmt.Sprintf("Failed to destroy object [%s] after multiple attempts. Moving on.", o.Desc().Name))
			return fmt.Errorf("failed to destroy object [%s] after multiple attempts", o.Desc().Name)
		}

		x, y := ui.GameCoordsToScreenCords(o.Position.X, o.Position.Y)
		ctx.HID.Click(game.LeftButton, x, y)
		attempts++
		utils.Sleep(100)
	}
}

func isBreakable(name object.Name) bool {
	breakableObjects := []object.Name{
		object.Barrel, object.Urn2, object.Urn3, object.Casket,
		object.Casket5, object.Casket6, object.LargeUrn1, object.LargeUrn4,
		object.LargeUrn5, object.Crate, object.HollowLog, object.Sarcophagus,
	}
	for _, n := range breakableObjects {
		if n == name {
			return true
		}
	}
	return false
}

// handleDoorsInPath manages interactions with doors in the bot's path.
func handleDoorsInPath(dest data.Position, openedDoors map[object.Name]data.Position) (data.Position, *data.Object, error) {
	ctx := context.Get()

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
	return data.Position{}, nil, nil
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

	if ctx.Data.PlayerUnit.States.HasState(state.Amplifydamage) ||
		ctx.Data.PlayerUnit.States.HasState(state.Lowerresist) ||
		ctx.Data.PlayerUnit.States.HasState(state.Decrepify) {

		ctx.Logger.Debug("Curse detected on player. Prioritizing finding any shrine to break it.")

		var closestCurseBreakingShrine *data.Object
		minDistance := math.MaxFloat64

		for _, o := range ctx.Data.Objects {
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

	var closestAlwaysTakeShrine *data.Object
	minDistance := math.MaxFloat64
	for _, o := range ctx.Data.Objects {
		if o.IsShrine() && o.Selectable {
			for _, sType := range alwaysTakeShrines {
				if sType == o.Shrine.ShrineType {
					if sType == object.HealthShrine && ctx.Data.PlayerUnit.HPPercent() > 95 {
						continue
					}
					if sType == object.ManaShrine && ctx.Data.PlayerUnit.MPPercent() > 95 {
						continue
					}
					if sType == object.RefillShrine && ctx.Data.PlayerUnit.HPPercent() > 95 && ctx.Data.PlayerUnit.MPPercent() > 95 {
						continue
					}

					distance := float64(ctx.PathFinder.DistanceFromMe(o.Position))
					if distance < minDistance {
						minDistance = distance
						closestAlwaysTakeShrine = &o
					}
				}
			}
		}
	}

	if closestAlwaysTakeShrine != nil {
		return closestAlwaysTakeShrine
	}

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
					closestShrine := &o
					return closestShrine
				}
			}
		}
	}

	return nil
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