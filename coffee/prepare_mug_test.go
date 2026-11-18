package coffee

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"beanjamin/coffee/order"
)

func TestHotWaterPhases(t *testing.T) {
	phases := hotWaterPhases()
	wantSteps := []string{
		stepLockingPortafilter, stepReleasingFilter, stepPlacingMug, stepAddingHotWater,
		stepServing, stepGrabbingFilter, stepUnlockingPortafilter,
	}
	if len(phases) != len(wantSteps) {
		t.Fatalf("hotWaterPhases() has %d phases, want %d", len(phases), len(wantSteps))
	}
	spans := map[string]bool{}
	for i, p := range phases {
		if p.step != wantSteps[i] {
			t.Errorf("phase %d step = %q, want %q", i+1, p.step, wantSteps[i])
		}
		if prefix := fmt.Sprintf("step %d/7: ", i+1); !strings.HasPrefix(p.progress, prefix) {
			t.Errorf("phase %d progress = %q, want prefix %q", i+1, p.progress, prefix)
		}
		switch p.step {
		case stepGrinding, stepTamping, stepBrewing, stepCleaning:
			t.Errorf("phase %d is %q; hot water brews no coffee", i+1, p.step)
		}
		if p.span == "" || spans[p.span] {
			t.Errorf("phase %d span %q is empty or repeated", i+1, p.span)
		}
		spans[p.span] = true
	}
	if stepPlacingMug != "Placing mug" || stepAddingHotWater != "Adding hot water" {
		t.Errorf("mug step labels = %q, %q", stepPlacingMug, stepAddingHotWater)
	}
}

func TestServeMugActionsRequireMugDrinks(t *testing.T) {
	s, _ := newTestCoffee(t, nil)
	for _, name := range []string{"serve_americano", "serve_hot_water"} {
		fn, ok := s.actionFuncs()[name]
		if !ok {
			t.Fatalf("execute_action %q is not registered", name)
		}
		if err := fn(context.Background(), context.Background()); err == nil || !strings.Contains(err.Error(), "can_serve_mug_drinks") {
			t.Errorf("%s: err = %v, want one naming can_serve_mug_drinks", name, err)
		}
	}
}

func TestHotWaterRoutinesCoverPhases(t *testing.T) {
	s := &beanjaminCoffee{}
	slot := -1
	routines := s.hotWaterRoutines(order.Order{Drink: "hot_water"}, &slot)
	for _, p := range hotWaterPhases() {
		if routines[p.span] == nil {
			t.Errorf("hot water phase span %q has no routine", p.span)
		}
	}
	if len(routines) != len(hotWaterPhases()) {
		t.Errorf("%d routines for %d phases", len(routines), len(hotWaterPhases()))
	}
}

// The hot water drink plans its portafilter phases in the chunks prepareDrink
// uses for them: the lock with the release, the re-grab with the unlock.
func TestHotWaterPhaseChunks(t *testing.T) {
	want := map[string]string{
		stepLockingPortafilter:   chunkPreparePortafilter,
		stepReleasingFilter:      chunkPreparePortafilter,
		stepGrabbingFilter:       chunkRetrievePortafilter,
		stepUnlockingPortafilter: chunkRetrievePortafilter,
	}
	for _, p := range hotWaterPhases() {
		if p.chunk != want[p.step] {
			t.Errorf("phase %q chunk = %q, want %q", p.step, p.chunk, want[p.step])
		}
	}
}
