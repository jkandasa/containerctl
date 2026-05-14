package reconcile

import (
	"context"
	"fmt"

	"github.com/jkandasa/containerctl/internal/config"
	rt "github.com/jkandasa/containerctl/internal/runtime"
)

type ActionType string

const (
	ActionCreate     ActionType = "create"
	ActionRecreate   ActionType = "recreate"
	ActionSkip       ActionType = "skip"
	ActionRemove     ActionType = "remove"
	ActionDisabled    ActionType = "disabled"    // disabled via state file; kept stopped
	ActionDeclaredOff ActionType = "declared-off" // disabled: true in YAML, not on host
)

type ContainerAction struct {
	Name       string
	FullName   string
	Action     ActionType
	Reason     string
	Spec       *config.Container
	RunningID  string
	RunningImg string
}

type NetworkAction struct {
	Name     string
	FullName string
	Action   ActionType
	Spec     *config.Network
}

type Plan struct {
	Project   string
	Networks  []NetworkAction
	Containers []ContainerAction
	Warnings  []string
}

func (p *Plan) HasChanges() bool {
	for _, n := range p.Networks {
		if n.Action != ActionSkip {
			return true
		}
	}
	for _, c := range p.Containers {
		if c.Action != ActionSkip && c.Action != ActionDisabled && c.Action != ActionDeclaredOff {
			return true
		}
	}
	return false
}

func Build(ctx context.Context, stack *config.Stack, runtime rt.Runtime, names []string, disabledNames map[string]bool) (*Plan, error) {
	plan := &Plan{Project: stack.Project}

	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}
	filterByName := len(names) > 0

	// --- networks ---
	managedNets, err := runtime.ListNetworks(ctx, rt.Filters{
		Labels: map[string]string{
			rt.LabelManaged: "true",
			rt.LabelProject: stack.Project,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("list networks: %w", err)
	}
	managedNetsByName := make(map[string]rt.NetworkInfo, len(managedNets))
	for _, n := range managedNets {
		lname := n.Labels[rt.LabelName]
		managedNetsByName[lname] = n
	}

	declaredNets := make(map[string]bool)
	for i := range stack.Networks {
		n := &stack.Networks[i]
		declaredNets[n.Name] = true
		fullName := config.NetworkFullName(stack.Project, n.Name)
		if _, exists := managedNetsByName[n.Name]; exists {
			plan.Networks = append(plan.Networks, NetworkAction{
				Name: n.Name, FullName: fullName, Action: ActionSkip, Spec: n,
			})
		} else {
			plan.Networks = append(plan.Networks, NetworkAction{
				Name: n.Name, FullName: fullName, Action: ActionCreate, Spec: n,
			})
		}
	}
	// orphaned managed networks — only clean up on a full apply (no name filter)
	if !filterByName {
		for lname, info := range managedNetsByName {
			if !declaredNets[lname] {
				plan.Networks = append(plan.Networks, NetworkAction{
					Name: lname, FullName: info.Name, Action: ActionRemove,
				})
			}
		}
	}

	// --- containers ---
	managedCtrs, err := runtime.ListContainers(ctx, rt.Filters{
		Labels: map[string]string{
			rt.LabelManaged: "true",
			rt.LabelProject: stack.Project,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}
	managedByName := make(map[string]rt.ContainerInfo, len(managedCtrs))
	for _, c := range managedCtrs {
		lname := c.Labels[rt.LabelName]
		managedByName[lname] = c
	}

	// build a set of declared container names (for depends_on validation)
	declaredNames := make(map[string]bool, len(stack.Containers))
	for _, c := range stack.Containers {
		declaredNames[c.Name] = true
	}

	// effectivelyDisabled merges YAML-disabled names with state-file disabled names
	// so depends_on warnings cover both sources.
	effectivelyDisabled := make(map[string]bool, len(disabledNames))
	for k := range disabledNames {
		effectivelyDisabled[k] = true
	}

	declaredSet := make(map[string]bool, len(stack.Containers))
	for i := range stack.Containers {
		c := &stack.Containers[i]
		declaredSet[c.Name] = true

		if filterByName && !nameSet[c.Name] {
			continue
		}

		fullName := config.ContainerFullName(stack.Project, c.Name)
		running, exists := managedByName[c.Name]

		// check for unmanaged conflict
		if !exists {
			conflict, err := runtime.InspectContainer(ctx, fullName)
			if err != nil {
				return nil, fmt.Errorf("inspect %s: %w", fullName, err)
			}
			if conflict != nil && conflict.Labels[rt.LabelManaged] != "true" {
				return nil, fmt.Errorf(
					"container %q exists but is not managed by containerctl; remove it first",
					fullName,
				)
			}
		}

		// YAML disabled: true
		if c.Disabled {
			if exists {
				plan.Containers = append(plan.Containers, ContainerAction{
					Name: c.Name, FullName: fullName, Action: ActionRemove,
					Reason: "disabled: true in YAML", Spec: c, RunningID: running.ID,
				})
			} else {
				plan.Containers = append(plan.Containers, ContainerAction{
					Name: c.Name, FullName: fullName, Action: ActionDeclaredOff, Spec: c,
				})
			}
			effectivelyDisabled[c.Name] = true
			continue
		}

		// state-file disabled (persistent off via containerctl disable)
		if disabledNames[c.Name] {
			plan.Containers = append(plan.Containers, ContainerAction{
				Name: c.Name, FullName: fullName, Action: ActionDisabled,
				Reason: "disabled via state file", Spec: c, RunningID: running.ID,
				RunningImg: running.Image,
			})
			continue
		}

		if !exists {
			plan.Containers = append(plan.Containers, ContainerAction{
				Name: c.Name, FullName: fullName, Action: ActionCreate, Spec: c,
			})
			continue
		}

		expectedHash := config.Hash(c)
		if running.Labels[rt.LabelConfigHash] == expectedHash {
			plan.Containers = append(plan.Containers, ContainerAction{
				Name: c.Name, FullName: fullName, Action: ActionSkip, Spec: c,
				RunningID: running.ID, RunningImg: running.Image,
			})
		} else {
			plan.Containers = append(plan.Containers, ContainerAction{
				Name: c.Name, FullName: fullName, Action: ActionRecreate, Spec: c,
				RunningID: running.ID, RunningImg: running.Image,
				Reason: fmt.Sprintf("hash changed (%s → %s)", running.Labels[rt.LabelConfigHash], expectedHash),
			})
		}
	}

	// orphaned managed containers not in YAML — only clean up on a full apply (no name filter)
	if !filterByName {
		for lname, info := range managedByName {
			if !declaredSet[lname] {
				fullName := config.ContainerFullName(stack.Project, lname)
				plan.Containers = append(plan.Containers, ContainerAction{
					Name: lname, FullName: fullName, Action: ActionRemove,
					Reason: "not in stack.yaml", RunningID: info.ID,
				})
			}
		}
	}

	// depends_on validation warnings
	for _, c := range stack.Containers {
		if c.Disabled {
			continue
		}
		for _, dep := range c.DependsOn {
			if !declaredNames[dep] {
				return nil, fmt.Errorf("containers[%s]: depends_on references unknown container %q", c.Name, dep)
			}
			if effectivelyDisabled[dep] {
				plan.Warnings = append(plan.Warnings,
					fmt.Sprintf("containers[%s]: depends on %q which is disabled", c.Name, dep),
				)
			}
		}
	}

	// topo-sort containers for execution order
	if err := topoSort(plan, stack); err != nil {
		return nil, err
	}

	return plan, nil
}

func topoSort(plan *Plan, stack *config.Stack) error {
	byName := make(map[string]*ContainerAction, len(plan.Containers))
	for i := range plan.Containers {
		byName[plan.Containers[i].Name] = &plan.Containers[i]
	}

	specByName := make(map[string]*config.Container, len(stack.Containers))
	for i := range stack.Containers {
		specByName[stack.Containers[i].Name] = &stack.Containers[i]
	}

	inDegree := make(map[string]int, len(plan.Containers))
	adj := make(map[string][]string, len(plan.Containers))
	for i := range plan.Containers {
		name := plan.Containers[i].Name
		inDegree[name] = 0
		adj[name] = nil
	}
	for i := range plan.Containers {
		name := plan.Containers[i].Name
		spec := specByName[name]
		if spec == nil {
			continue
		}
		for _, dep := range spec.DependsOn {
			if _, ok := inDegree[dep]; !ok {
				continue
			}
			adj[dep] = append(adj[dep], name)
			inDegree[name]++
		}
	}

	queue := make([]string, 0)
	for name, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, name)
		}
	}

	sorted := make([]ContainerAction, 0, len(plan.Containers))
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if a, ok := byName[cur]; ok {
			sorted = append(sorted, *a)
		}
		for _, next := range adj[cur] {
			inDegree[next]--
			if inDegree[next] == 0 {
				queue = append(queue, next)
			}
		}
	}

	if len(sorted) != len(plan.Containers) {
		return fmt.Errorf("cycle detected in depends_on")
	}
	plan.Containers = sorted
	return nil
}
