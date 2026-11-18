package coffee

// The mug drinks' share of the order routine: the hot water drink's own phases
// and the Americano's serve, kept out of prepareDrink so the cup and iced flows
// there stay readable.

import (
	"context"
	"fmt"

	"go.viam.com/rdk/logging"

	"beanjamin/coffee/order"
)

// phaseRunner is prepareDrink's runPhase: it publishes the step label, logs
// the progress line and runs the phase in its own trace span.
type phaseRunner func(step, spanName, progress string, fn func(ctx, cancelCtx context.Context) error) error

// phaseSpec is one phase of a drink's routine as data: its step label, trace
// span name and progress line. Keep the label and the progress line in sync:
// both surface to the UI. Consecutive phases naming the same chunk are planned
// together as that chunk (inChunk), as prepareDrink groups its phases; "" plans
// the phase's motions on their own.
type phaseSpec struct{ step, span, progress, chunk string }

// hotWaterPhases is the hot water drink's routine. The gripper holds the
// portafilter between orders, so it locks the empty portafilter into the group
// head and lets go before it can pick up a mug, then takes it back at the end.
// No grinding, tamping, brewing or cleaning. The move home is the last phase,
// planned with the unlock as prepareDrink plans its own.
func hotWaterPhases() []phaseSpec {
	return []phaseSpec{
		{stepLockingPortafilter, spanLockingPortafilter, "step 1/8: locking empty portafilter", chunkPreparePortafilter},
		{stepReleasingFilter, spanReleasingFilter, "step 2/8: releasing filter", chunkPreparePortafilter},
		{stepPlacingMug, spanPlacingMugAtWater, "step 3/8: placing mug at the hot water", ""},
		{stepAddingHotWater, spanAddingHotWater, "step 4/8: adding hot water", ""},
		{stepServing, spanServing, "step 5/8: serving mug", ""},
		{stepGrabbingFilter, spanGrabbingFilter, "step 6/8: grabbing filter", chunkRetrievePortafilter},
		{stepUnlockingPortafilter, spanUnlockingPortafilter, "step 7/8: unlocking portafilter", chunkRetrievePortafilter},
		{stepFinishingUp, spanMovingHome, "step 8/8: moving to home pose", chunkRetrievePortafilter},
	}
}

// Chunks the hot water phases are planned in: the lock pivot with the release
// after it, and the re-grab with the unlock pivot and the move home, as
// prepareDrink plans them.
const (
	chunkPreparePortafilter  = "prepare portafilter"
	chunkRetrievePortafilter = "retrieve portafilter"
)

// Span names of the hot water phases; hotWaterPhases lists them and
// hotWaterRoutines binds each to its routine.
const (
	spanLockingPortafilter   = "locking_portafilter"
	spanReleasingFilter      = "releasing_filter"
	spanPlacingMugAtWater    = "placing_mug_at_water"
	spanAddingHotWater       = "adding_hot_water"
	spanServing              = "serving"
	spanGrabbingFilter       = "grabbing_filter"
	spanUnlockingPortafilter = "unlocking_portafilter"
	spanMovingHome           = "moving_home"
)

// hotWaterRoutines binds each hot water phase span to its routine. servedSlot
// receives where the serving phase put the mug.
func (s *beanjaminCoffee) hotWaterRoutines(o order.Order, servedSlot *int) map[string]func(ctx, cancelCtx context.Context) error {
	return map[string]func(ctx, cancelCtx context.Context) error{
		spanLockingPortafilter: s.lockPortaFilter,
		spanReleasingFilter:    s.releaseFilter,
		spanPlacingMugAtWater:  s.placeMugAtWater,
		spanAddingHotWater: func(ctx, cancelCtx context.Context) error {
			return s.dispenseHotWater(ctx, cancelCtx, o.Drink)
		},
		spanServing: func(ctx, cancelCtx context.Context) (err error) {
			*servedSlot, err = s.grabAndShelve(ctx, cancelCtx, s.regrabMugAtWater)
			return err
		},
		spanGrabbingFilter:       s.grabFilter,
		spanUnlockingPortafilter: s.unlockPortaFilter,
		spanMovingHome: func(ctx, cancelCtx context.Context) error {
			return s.executeStep(ctx, cancelCtx, Step{PoseName: filterPoseHome, PoseSwitch: s.filterSw})
		},
	}
}

// prepareHotWater runs the hot water drink: the phases of hotWaterPhases, each
// bound to its routine by span, ending with the home move. Called from prepareDrink
// after its shared setup, so the same defers (failed-step capture, pause on
// fault, flow end) cover it. A mug drink always goes to the shelf, never the
// delivery cart.
func (s *beanjaminCoffee) prepareHotWater(ctx, cancelCtx context.Context, o order.Order, runPhase phaseRunner) error {
	drink := o.Drink
	logger := s.activeOrderLogger()

	servedSlot := -1
	routines := s.hotWaterRoutines(o, &servedSlot)
	runOne := func(p phaseSpec) error {
		fn, ok := routines[p.span]
		if !ok {
			return fmt.Errorf("prepare_hot_water: no routine for phase %q", p.span)
		}
		if err := runPhase(p.step, p.span, p.progress, fn); err != nil {
			return err
		}
		switch p.span {
		case spanPlacingMugAtWater:
			// A mug was pulled off the shelf; count it now so a later fault still credits real use.
			label, key := cupCounter(drink)
			s.countUsage(ctx, label, key, 1)
		case spanAddingHotWater:
			s.countUsage(ctx, "water", "water_used", waterDelta(drink))
		case spanServing:
			s.announceServed(ctx, logger, o, servedSlot, false)
		}
		return nil
	}
	phases := hotWaterPhases()
	for i := 0; i < len(phases); {
		j := i + 1
		for j < len(phases) && phases[i].chunk != "" && phases[j].chunk == phases[i].chunk {
			j++
		}
		group := phases[i:j]
		runGroup := func() error {
			for _, p := range group {
				if err := runOne(p); err != nil {
					return err
				}
			}
			return nil
		}
		var err error
		if name := group[0].chunk; name != "" {
			err = s.inChunk(name, runGroup)
		} else {
			err = runGroup()
		}
		if err != nil {
			return err
		}
		i = j
	}
	logger.Infof("%s preparation complete", drink)
	return nil
}

// serveAmericano finishes an Americano once the lungo has poured into the mug
// under the group head: grasp the mug (approaching it first unless the arm
// already lined up during the pour), slide it to the hot water spout, set it
// down, top it up with hot water, and pick it back up — one chunk of its own,
// the top-up stretch — then hand the held mug to placeDrink to set down.
// Returns where placeDrink put it.
//
// The top-up runs before the placement rather than as its grab, so a slot that
// fails to plan only re-records the placement, never the top-up.
func (s *beanjaminCoffee) serveAmericano(ctx, cancelCtx context.Context, approached bool, placeDrink servePlacement) (int, error) {
	logger := s.activeOrderLogger()
	if err := s.inChunk(chunkAmericanoTopUp, func() error {
		return s.topUpAmericano(ctx, cancelCtx, approached, logger)
	}); err != nil {
		return -1, err
	}
	s.setStep(stepServing)
	// The mug is already in the gripper: no grab.
	return placeDrink(ctx, cancelCtx, nil)
}

// chunkAmericanoTopUp is the chunk the Americano's top-up is planned in.
const chunkAmericanoTopUp = "top up americano"

// topUpAmericano is serveAmericano's top-up stretch: it ends with the topped-up
// mug in the gripper, clear of the spout.
func (s *beanjaminCoffee) topUpAmericano(ctx, cancelCtx context.Context, approached bool, logger logging.Logger) error {
	mug := s.mugVessel()
	if !approached {
		if err := s.approachBrewedVessel(ctx, cancelCtx, mug); err != nil {
			return err
		}
	}
	if err := s.graspBrewedInPlace(ctx, cancelCtx, mug); err != nil {
		return err
	}
	if err := s.slideMugToWater(ctx, cancelCtx); err != nil {
		return err
	}
	if err := s.setMugAtWater(ctx, cancelCtx); err != nil {
		return err
	}
	s.setStep(stepAddingHotWater)
	s.handOffEffect(func() { logger.Info("step 6c/9: adding hot water") })
	if err := s.dispenseHotWater(ctx, cancelCtx, "americano"); err != nil {
		return err
	}
	return s.regrabMugAtWater(ctx, cancelCtx)
}

// serveHotWater is the serve_hot_water action: the hot water drink's mug
// phases end to end (place a mug at the spout, add hot water, pick it back up
// and serve it). Like serve_iced_coffee it assumes the gripper is free, i.e.
// the portafilter is already out of it.
func (s *beanjaminCoffee) serveHotWater(ctx, cancelCtx context.Context) (int, error) {
	if err := s.placeMugAtWater(ctx, cancelCtx); err != nil {
		return -1, err
	}
	if err := s.dispenseHotWater(ctx, cancelCtx, "hot_water"); err != nil {
		return -1, err
	}
	return s.grabAndShelve(ctx, cancelCtx, s.regrabMugAtWater)
}
