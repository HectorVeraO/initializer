package plan

import (
	"fmt"
	"strings"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/catalog"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/names"
)

// LockNames returns every live slot of p, component.* included, for the lock.
func (p Plan) LockNames() map[string]string {
	locked := make(map[string]string, len(p.Live))
	for slot := range p.Live {
		locked[slot] = p.Slots[slot]
	}
	return locked
}

// slots computes the slots of one pair: the backend component and its frontend.
// A slot used by a rule whose value no input gives yet is an internal error:
// the catalog and the generator disagree.
func (r *resolver) slots(stacks []ComponentPlan, src infraSource, others []ComponentPlan) (values map[string]string, live map[string]bool) {
	project := r.sel.Project.Name
	resolved := names.Resolved{Project: project, Components: map[string]string{}, Multi: r.multi}
	live = map[string]bool{names.SlotProject: true}
	selected := map[string]bool{}
	for _, component := range stacks {
		resolved.Components[component.Stack] = component.Name
		live[names.ComponentSlot(component.Stack)] = true
		selected[component.Stack] = true
	}
	values = resolved.Slots()
	for _, stack := range stacks {
		installed := map[string]bool{}
		for _, module := range stack.Modules {
			installed[module.Module] = true
		}
		for _, module := range stack.Modules {
			for _, rule := range module.Names {
				used, err := names.Slots(rule.Text)
				if err != nil {
					r.add(CodeInternalError, catalogSource, "/stacks", fmt.Sprintf("module %s: rule %q: %v", module.Module, rule.Text, err))
					continue
				}
				isLive := ruleIsLive(rule, installed, selected)
				for _, slot := range used {
					if _, known := values[slot]; !known {
						value, ok := r.defaultSlot(slot, src)
						if !ok {
							r.add(CodeInternalError, catalogSource, "/stacks",
								fmt.Sprintf("module %s: rule %q uses slot %q, which has no value source yet", module.Module, rule.Text, slot))
							continue
						}
						values[slot] = value
					}
					if isLive && !isUnselectedComponentSlot(slot, selected) && names.IsRecorded(slot) {
						live[slot] = true
					}
				}
			}
		}
	}
	r.fillOtherSlots(values, others, src)
	return values, live
}

// fillOtherSlots gives the pair a value for every slot a rule of another
// pair's modules uses, so a file transformed in this pair's context can apply
// every rule of the project. These slots are never live here.
func (r *resolver) fillOtherSlots(values map[string]string, others []ComponentPlan, src infraSource) {
	for _, other := range others {
		for _, module := range other.Modules {
			for _, rule := range module.Names {
				used, err := names.Slots(rule.Text)
				if err != nil {
					continue
				}
				for _, slot := range used {
					if _, known := values[slot]; known {
						continue
					}
					if value, ok := r.defaultSlot(slot, src); ok {
						values[slot] = value
					}
				}
			}
		}
	}
}

// ruleIsLive reports whether one of the blocks of rule is kept: no block, an
// installed module or a selected stack.
func ruleIsLive(rule catalog.NameRule, installed, selected map[string]bool) bool {
	for _, block := range rule.Blocks {
		kind, id, _ := strings.Cut(block, ":")
		switch {
		case block == "":
			return true
		case kind == "module" && installed[id]:
			return true
		case kind == "stack" && selected[id]:
			return true
		}
	}
	return false
}

// defaultSlot returns the value of a slot no input sets: the single form of a
// derived slot, the default component name of a stack that is not selected, or
// an instance value.
func (r *resolver) defaultSlot(slot string, src infraSource) (string, bool) {
	if value, isDerived := names.DerivedDefault(slot); isDerived {
		return value, true
	}
	stack, isComponent := strings.CutPrefix(slot, "component.")
	if isComponent {
		if _, known := manifest.LookupStack(stack); known {
			return defaultComponentName(r.sel.Project.Name, stack), true
		}
	}
	return r.defaultInfraSlot(slot, src)
}

// isUnselectedComponentSlot reports whether slot is the component slot of a
// stack that is not selected: such a slot is never recorded in the lock.
func isUnselectedComponentSlot(slot string, selected map[string]bool) bool {
	stack, isComponent := strings.CutPrefix(slot, "component.")
	return isComponent && !selected[stack]
}
