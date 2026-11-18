package coffee

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/golang/geo/r3"
	"go.viam.com/rdk/spatialmath"

	"beanjamin/coffee/geom"
)

func TestPickupGetters(t *testing.T) {
	if got := pickupMaxAttempts(0); got != defaultCupPickupMaxAttempts {
		t.Errorf("pickupMaxAttempts(0) = %d, want %d", got, defaultCupPickupMaxAttempts)
	}
	if got := pickupMaxAttempts(7); got != 7 {
		t.Errorf("pickupMaxAttempts(7) = %d, want 7", got)
	}
}

func TestHeldGeometriesCupIsOneBox(t *testing.T) {
	s, _ := newTestCoffee(t, nil)
	box, err := spatialmath.NewBox(spatialmath.NewPoseFromPoint(r3.Vector{X: 100, Y: 200, Z: 50}), r3.Vector{X: 70, Y: 70, Z: 90}, "cup")
	if err != nil {
		t.Fatal(err)
	}
	cand := geom.Candidate{Geom: box}
	got, err := s.cupPickupTarget().heldGeometries(cand)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != box {
		t.Fatalf("got %d geometries, want the candidate's own box", len(got))
	}
}

func TestHeldGeometriesMugAddsHandle(t *testing.T) {
	s, _ := newTestCoffee(t, &Config{
		MugDimensions:       &ContainerDimensions{DiameterMm: 80, HeightMm: 97},
		MugHandleDimensions: &HandleDimensions{DepthMm: 39, WidthMm: 15},
		MugGrabRelativePose: &RelativePose{},
	})
	body, err := spatialmath.NewBox(spatialmath.NewPoseFromPoint(r3.Vector{X: 100, Y: 200, Z: 50}), r3.Vector{X: 80, Y: 80, Z: 97}, "mug")
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.mugPickupTarget().heldGeometries(geom.Candidate{Geom: body})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d geometries, want body and handle", len(got))
	}
	h := got[1].Pose().Point()
	if math.Abs(h.X-100) > 1e-6 || math.Abs(h.Y-(200+59.5)) > 1e-6 {
		t.Errorf("handle centre = %+v, want X=100, Y=%v", h, 200+59.5)
	}
}

func TestMugPickupTargetFields(t *testing.T) {
	s, _ := newTestCoffee(t, nil)
	tg := s.mugPickupTarget()
	if tg.label != pickupLabelMug || tg.observeHomePose != mugPoseObserve || tg.shieldFrame != cleanMugAreaShieldFrameName {
		t.Errorf("unexpected target: %+v", tg)
	}
	if want := "I don't see a mug yet — please place one on the mug shelf. Trying again in 15 seconds."; tg.noItemSpeak != want {
		t.Errorf("noItemSpeak = %q", tg.noItemSpeak)
	}
	if want := "I can see a mug but I'm having trouble grabbing it — could you nudge it a little? Trying again in 15 seconds."; tg.unreachableSpeak != want {
		t.Errorf("unreachableSpeak = %q", tg.unreachableSpeak)
	}
}

func TestPickMugRequiresMugDrinks(t *testing.T) {
	s, _ := newTestCoffee(t, nil)
	err := s.actionFuncs()["pick_mug"](context.Background(), context.Background())
	if err == nil || !strings.Contains(err.Error(), "can_serve_mug_drinks") {
		t.Fatalf("err = %v, want one naming can_serve_mug_drinks", err)
	}
}
