package coffee

import (
	"testing"

	"github.com/golang/geo/r3"
	"go.viam.com/rdk/referenceframe"
	"go.viam.com/rdk/spatialmath"
)

func TestCupVesselMatchesToday(t *testing.T) {
	v := (&beanjaminCoffee{}).cupVessel()
	if v.label != "cup" || v.underApproach != "cup_under_machine_approach" || v.readyForCoffee != "cup_ready_for_coffee" {
		t.Fatalf("cup vessel = %+v", v)
	}
}

func TestMugVesselPoses(t *testing.T) {
	v := (&beanjaminCoffee{}).mugVessel()
	if v.label != "mug" || v.underApproach != "mug_under_machine_approach" || v.readyForCoffee != "mug_ready_for_coffee" {
		t.Fatalf("mug vessel = %+v", v)
	}
}

func TestConfiguredMugGeometryHasHandle(t *testing.T) {
	fs := gripPointStaticFS(t, spatialmath.NewPoseFromPoint(r3.Vector{X: 170, Y: -300, Z: 250}))
	s := heldGeomService(t, fs)
	s.cfg.MugDimensions = &ContainerDimensions{DiameterMm: 80, HeightMm: 90}
	s.cfg.MugHandleDimensions = &HandleDimensions{DepthMm: 30, WidthMm: 20}
	s.cfg.MugGrabRelativePose = &RelativePose{X: 5, Z: -30}

	geoms, err := s.scene().configuredMugGeometries(placement{inputs: referenceframe.NewZeroInputs(fs)}, s.cfg)
	if err != nil {
		t.Fatalf("configuredMugGeometries: %v", err)
	}
	if len(geoms) != 2 {
		t.Fatalf("got %d geometries, want body + handle", len(geoms))
	}
	requireVecEqual(t, geoms[0].Pose().Point(), r3.Vector{X: 165, Y: -300, Z: 280}, 1e-6)
}
