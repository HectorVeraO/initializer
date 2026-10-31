package plan

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/ports"
)

// CodePortBandExhausted reports a computed port offset that clashes with
// another one or passes the band.
const CodePortBandExhausted = "PORT_BAND_EXHAUSTED"

// highestOffset is the last offset an instance port may use; 99 is the band bound.
const highestOffset = 98

// PortPlan is the effective port band of a project.
type PortPlan struct {
	// RefBand is the catalog's reference band (101); 0 when the catalog
	// declares no ports.
	RefBand int
	// Band is the effective band: selection ports.band, else 101.
	Band int
}

// catalogPort is one declared port of the catalog with its owning stack.
type catalogPort struct {
	stack string
	ports.Port
}

// catalogPorts returns every port the catalog declares, sorted by offset.
func (r *resolver) catalogPorts() []catalogPort {
	var declared []catalogPort
	for _, stack := range r.cat.Stacks {
		for _, module := range stack.Modules {
			for _, port := range module.Ports {
				declared = append(declared, catalogPort{stack: stack.Stack, Port: port})
			}
		}
	}
	sort.Slice(declared, func(i, j int) bool { return declared[i].Offset < declared[j].Offset })
	return declared
}

// originOf returns the origin port of a stack.
func originOf(declared []catalogPort, stack string) (catalogPort, bool) {
	for _, port := range declared {
		if port.stack == stack && port.Origin {
			return port, true
		}
	}
	return catalogPort{}, false
}

// portBand returns the effective band, reporting a band outside the range.
func (r *resolver) portBand() PortPlan {
	band, isValid := r.effectiveBand()
	if !isValid {
		r.add(manifest.CodeSelectionInvalid, selectionSource, "/ports/band", "must be an integer from 100 to 654")
	}
	return PortPlan{RefBand: r.cat.PortBand, Band: band}
}

// effectiveBand returns the band of the selection, else the default, and
// whether the selected band is in range. An out-of-range band falls back to the
// default. It reports no issue.
func (r *resolver) effectiveBand() (band int, isValid bool) {
	if r.sel.Ports == nil {
		return ports.DefaultBand, true
	}
	if !ports.ValidBand(r.sel.Ports.Band) {
		return ports.DefaultBand, false
	}
	return r.sel.Ports.Band, true
}

// tokenOffsets maps every declared offset to the offset a token carries in the
// context of one component: a port of its own stack at its index, a port of the
// bound partner's stack at the partner's index, the origin port of a stack
// that is not present at the component's own origin, any other port of a
// stack that is not present at its offset.
func tokenOffsets(declared []catalogPort, component ComponentPlan, partner *ComponentPlan) map[int]int {
	offsets := map[int]int{}
	own, hasOwnOrigin := originOf(declared, component.Stack)
	for _, port := range declared {
		switch {
		case port.stack == component.Stack:
			offsets[port.Offset] = port.OffsetAt(component.Index)
		case partner != nil && port.stack == partner.Stack:
			offsets[port.Offset] = port.OffsetAt(partner.Index)
		case port.Origin && hasOwnOrigin:
			offsets[port.Offset] = own.OffsetAt(component.Index)
		default:
			offsets[port.Offset] = port.Offset
		}
	}
	return offsets
}

// partnerOf returns the bound partner of component among components.
func partnerOf(components []ComponentPlan, component ComponentPlan) *ComponentPlan {
	for i := range components {
		if components[i].Name == component.Partner && component.Partner != "" {
			return &components[i]
		}
	}
	return nil
}

// fillPorts sets the host ports, the token offsets and the public origin of
// every component.
func (r *resolver) fillPorts(components []ComponentPlan, layout PortPlan) {
	declared := r.catalogPorts()
	for i := range components {
		component := &components[i]
		partner := partnerOf(components, *component)
		component.Ports = map[string]int{}
		for _, module := range component.Modules {
			for _, port := range module.Ports {
				component.Ports[port.Name] = layout.Band*100 + port.OffsetAt(component.Index)
			}
		}
		component.TokenOffsets = tokenOffsets(declared, *component, partner)
		if component.Role != roleBackend {
			continue
		}
		originPort, hasOrigin := originOf(declared, component.Stack)
		if partner != nil {
			if partnerOrigin, hasPartnerOrigin := originOf(declared, partner.Stack); hasPartnerOrigin {
				originPort, hasOrigin = partnerOrigin, true
				component.Origin = "http://localhost:" + strconv.Itoa(layout.Band*100+originPort.OffsetAt(partner.Index))
				continue
			}
		}
		if hasOrigin {
			component.Origin = "http://localhost:" + strconv.Itoa(layout.Band*100+originPort.OffsetAt(component.Index))
		}
	}
}

// portClash is one installed port that cannot take its computed offset.
type portClash struct {
	component string
	index     int
	stack     string
	port      string
	detail    string
}

// findPortClashes returns the first clash of each port of components, in
// component order: a computed offset of an installed port that another port
// already uses or that passes the band.
func findPortClashes(components []ComponentPlan) []portClash {
	type user struct {
		component string
		index     int
		port      string
	}
	var clashes []portClash
	used := map[int]user{}
	for _, component := range components {
		for _, module := range component.Modules {
			for _, port := range module.Ports {
				offset := port.OffsetAt(component.Index)
				here := user{component: component.Name, index: component.Index, port: port.Name}
				clash := portClash{component: here.component, index: here.index, stack: component.Stack, port: here.port}
				if offset > highestOffset {
					clash.detail = fmt.Sprintf("would use offset %d; the band has 0-%d", offset, highestOffset)
					clashes = append(clashes, clash)
					continue
				}
				first, taken := used[offset]
				if taken && port.Shared && first.port == port.Name {
					continue
				}
				if taken {
					clash.detail = fmt.Sprintf("would use offset %d, which port %s of %s (index %d) uses", offset, first.port, first.component, first.index)
					clashes = append(clashes, clash)
					continue
				}
				used[offset] = here
			}
		}
	}
	return clashes
}

// largestFit returns the most components of stack the band holds with the same
// modules per component and the components of every other stack unchanged: the
// components of stack past the returned count are dropped from the layout.
func largestFit(components []ComponentPlan, stack string) int {
	count := 0
	for _, component := range components {
		if component.Stack == stack {
			count++
		}
	}
	for fit := count; fit > 0; fit-- {
		kept := make([]ComponentPlan, 0, len(components))
		for _, component := range components {
			if component.Stack != stack || component.Index < fit {
				kept = append(kept, component)
			}
		}
		if len(findPortClashes(kept)) == 0 {
			return fit
		}
	}
	return 0
}

// checkPortBand reports PORT_BAND_EXHAUSTED once per clash or overflow: a
// computed offset of an installed port that another port already uses or that
// passes the band. The number of components of a stack has no other limit.
func (r *resolver) checkPortBand(components []ComponentPlan) {
	for _, clash := range findPortClashes(components) {
		fit := largestFit(components, clash.stack)
		r.issues = append(r.issues, issue.Issue{
			Code:    CodePortBandExhausted,
			Path:    issue.Path(selectionSource, "/components"),
			Message: fmt.Sprintf("port %s of %s (index %d) %s", clash.port, clash.component, clash.index, clash.detail),
			Hint: fmt.Sprintf("the band holds offsets 0-%d and each component takes its own: with these modules and the other components of this selection at most %d %s components fit; use fewer, or a catalog whose port strides fit the band",
				highestOffset, fit, clash.stack),
		})
	}
}
