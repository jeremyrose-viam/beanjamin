package coffee

import "context"

// vessel is what differs between a cup and a mug on the way under the group
// head and back out: how it is picked, the claws poses in front of and under
// the spout, and how its held geometry is modeled when nothing was cached.
type vessel struct {
	label            string                                     // pickupLabelCup / pickupLabelMug
	pick             func(ctx, cancelCtx context.Context) error // pickCupDynamic / pickMugDynamic
	underApproach    string                                     // claws pose in front of the group head
	readyForCoffee   string                                     // claws pose under the group head
	attachConfigured func(ctx context.Context) error            // fallback when nothing is cached
	dimsField        string                                     // config key attachConfigured models from, for messages
}

func (s *beanjaminCoffee) cupVessel() vessel {
	return vessel{
		label:            pickupLabelCup,
		pick:             s.pickCupDynamic,
		underApproach:    clawPoseCupUnderMachineApproach,
		readyForCoffee:   clawPoseCupReadyForCoffee,
		attachConfigured: s.attachConfiguredCupGeometry,
		dimsField:        "cup_dimensions",
	}
}

func (s *beanjaminCoffee) mugVessel() vessel {
	return vessel{
		label:            pickupLabelMug,
		pick:             s.pickMugDynamic,
		underApproach:    clawPoseMugUnderMachineApproach,
		readyForCoffee:   clawPoseMugReadyForCoffee,
		attachConfigured: s.attachConfiguredMugGeometry,
		dimsField:        "mug_dimensions",
	}
}
