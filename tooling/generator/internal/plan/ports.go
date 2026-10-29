package plan

import (
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/ports"
)

// PortPlan is the effective port layout of a project.
type PortPlan struct {
	// RefBand is the catalog's reference band (101); 0 when the catalog
	// declares no ports.
	RefBand int
	// Band is the effective band: selection ports.band, else 101.
	Band int
	// Offsets is the origin-rule map applied with the band ({20: 0} without a frontend).
	Offsets map[int]int
	// Origin is the public origin host port (Band*100 + the selected origin offset).
	Origin int
}

// originOffsets returns the offset of the origin port of each stack of the
// catalog that declares one.
func (r *resolver) originOffsets() map[string]int {
	origins := map[string]int{}
	for _, stack := range r.cat.Stacks {
		for _, module := range stack.Modules {
			for _, port := range module.Ports {
				if port.Origin {
					origins[stack.Stack] = port.Offset
				}
			}
		}
	}
	return origins
}

// portPlan applies the origin rule: the origin port is the origin of the
// selected frontend stack, else of the backend stack, and the origin offset of
// every unselected stack maps to it.
func (r *resolver) portPlan(stacks []ComponentPlan) PortPlan {
	band := ports.DefaultBand
	if r.sel.Ports != nil {
		band = r.sel.Ports.Band
		if !ports.ValidBand(band) {
			r.add(manifest.CodeSelectionInvalid, selectionSource, "/ports/band", "must be an integer from 100 to 654")
			band = ports.DefaultBand
		}
	}
	plan := PortPlan{RefBand: r.cat.PortBand, Band: band}
	origins := r.originOffsets()
	selected := map[string]bool{}
	originStack := ""
	for _, stack := range stacks {
		selected[stack.Stack] = true
		_, hasOrigin := origins[stack.Stack]
		isBetter := hasOrigin && (originStack == "" || stack.Role == roleFrontend)
		if isBetter {
			originStack = stack.Stack
		}
	}
	originOffset := 0
	if originStack != "" {
		originOffset = origins[originStack]
	}
	for stack, offset := range origins {
		if selected[stack] || offset == originOffset {
			continue
		}
		if plan.Offsets == nil {
			plan.Offsets = map[int]int{}
		}
		plan.Offsets[offset] = originOffset
	}
	plan.Origin = band*100 + originOffset
	return plan
}
