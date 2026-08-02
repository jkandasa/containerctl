package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/jkandasa/containerctl/internal/config"
	"github.com/spf13/cobra"
)

// flagLabels is the shared destination for -l/--label on commands that support
// label selection. Only the executed command's flags are parsed, so sharing
// the slice across commands is safe.
var flagLabels []string

func addLabelFlag(cmd *cobra.Command) {
	cmd.Flags().StringArrayVarP(&flagLabels, "label", "l", nil,
		`select containers by stack labels (kubectl-style). KEY, !KEY, KEY=VALUE, KEY!=VALUE; comma-separate; repeat -l (all AND). Example: -l release,environment=production`)
}

type labelOp int

const (
	labelOpEq labelOp = iota // KEY=VALUE
	labelOpNeq               // KEY!=VALUE
	labelOpExists            // KEY  (key must be present)
	labelOpNotExists         // !KEY (key must be absent)
)

// labelSelector is one term of a label filter.
type labelSelector struct {
	Key   string
	Op    labelOp
	Value string
}

// parseLabelFilters parses -l/--label arguments into selectors.
//
// Each flag value may contain comma-separated terms (AND). Multiple flag
// values are also AND'd. Supported operators (kubectl-compatible):
//
//	KEY         key must be present (any value)
//	!KEY        key must be absent
//	KEY=VALUE   key present and equal VALUE
//	KEY!=VALUE  key absent, or present with a different value
//
// Example: -l release,environment=production
func parseLabelFilters(specs []string) ([]labelSelector, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	var out []labelSelector
	for _, spec := range specs {
		for _, part := range strings.Split(spec, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			sel, err := parseOneLabelSelector(part)
			if err != nil {
				return nil, err
			}
			out = append(out, sel)
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func parseOneLabelSelector(part string) (labelSelector, error) {
	// Check != before = so "a!=b" is not parsed as key "a!" value "b".
	if k, v, ok := strings.Cut(part, "!="); ok {
		k = strings.TrimSpace(k)
		if k == "" {
			return labelSelector{}, fmt.Errorf("invalid -l %q: empty key in KEY!=VALUE", part)
		}
		return labelSelector{Key: k, Op: labelOpNeq, Value: strings.TrimSpace(v)}, nil
	}
	if k, v, ok := strings.Cut(part, "="); ok {
		k = strings.TrimSpace(k)
		if k == "" {
			return labelSelector{}, fmt.Errorf("invalid -l %q: empty key in KEY=VALUE", part)
		}
		return labelSelector{Key: k, Op: labelOpEq, Value: strings.TrimSpace(v)}, nil
	}
	// Existence / non-existence: KEY or !KEY
	if strings.HasPrefix(part, "!") {
		k := strings.TrimSpace(part[1:])
		if k == "" {
			return labelSelector{}, fmt.Errorf("invalid -l %q: empty key in !KEY", part)
		}
		if strings.ContainsAny(k, " \t") {
			return labelSelector{}, fmt.Errorf("invalid -l %q: key must not contain whitespace", part)
		}
		return labelSelector{Key: k, Op: labelOpNotExists}, nil
	}
	k := part
	if k == "" {
		return labelSelector{}, fmt.Errorf("invalid -l %q: empty key", part)
	}
	if strings.ContainsAny(k, " \t") {
		return labelSelector{}, fmt.Errorf("invalid -l %q: key must not contain whitespace", part)
	}
	return labelSelector{Key: k, Op: labelOpExists}, nil
}

// containerMatchesLabels reports whether c satisfies every selector.
// Matching uses the stack YAML labels only (kubectl-style semantics):
//
//	KEY         key must be present
//	!KEY        key must be absent
//	KEY=VALUE   key must be present and equal VALUE
//	KEY!=VALUE  key is absent, or present with a different value
func containerMatchesLabels(c *config.Container, sels []labelSelector) bool {
	if len(sels) == 0 {
		return true
	}
	for _, s := range sels {
		actual, present := "", false
		if c.Labels != nil {
			actual, present = c.Labels[s.Key]
		}
		switch s.Op {
		case labelOpExists:
			if !present {
				return false
			}
		case labelOpNotExists:
			if present {
				return false
			}
		case labelOpEq:
			if !present || actual != s.Value {
				return false
			}
		case labelOpNeq:
			if present && actual == s.Value {
				return false
			}
		}
	}
	return true
}

// selectContainerNames resolves the set of logical container names to act on.
//
//	names              positional args (optional)
//	labelSpecs         -l/--label selectors (optional, AND)
//	requireSelect      when true, at least one of names, labels, or all must be
//	                   set (start/stop/restart). When false, no selectors means
//	                   "entire project" (apply/down/status).
//	all                --all: start from every declared container in the stack
//	allowUnknownNames  when true, names missing from the stack are kept as-is
//	                   if no label filter is active (status orphans). Unknown
//	                   names are dropped when labels are also required (they
//	                   cannot match stack labels).
//
// Matching labels always uses stack YAML. When names and labels are both
// given, the result is the intersection.
//
// Returns (selectedNames, filtered, error). filtered is true when the caller
// should limit work to selectedNames (partial scope). When filtered is false,
// selectedNames is nil and the command should operate on the full project.
func selectContainerNames(stack *config.Stack, names, labelSpecs []string, requireSelect, all, allowUnknownNames bool) ([]string, bool, error) {
	sels, err := parseLabelFilters(labelSpecs)
	if err != nil {
		return nil, false, err
	}

	hasNames := len(names) > 0
	hasLabels := len(sels) > 0

	if requireSelect && !hasNames && !hasLabels && !all {
		return nil, false, fmt.Errorf("specify at least one container name, -l/--label, or --all")
	}

	if !hasNames && !hasLabels && !all {
		// full project, unfiltered
		return nil, false, nil
	}

	// Candidate set from the stack; unknown keeps names not in the stack.
	var candidates []*config.Container
	var unknown []string
	if hasNames {
		seen := make(map[string]bool, len(names))
		for _, n := range names {
			if seen[n] {
				continue
			}
			seen[n] = true
			c := stack.ContainerByName(n)
			if c == nil {
				if allowUnknownNames && !hasLabels {
					unknown = append(unknown, n)
					continue
				}
				if allowUnknownNames && hasLabels {
					// cannot match labels without a stack entry
					continue
				}
				return nil, false, fmt.Errorf("container %q not found in stack", n)
			}
			candidates = append(candidates, c)
		}
	} else {
		// labels only, or --all (with optional labels)
		for i := range stack.Containers {
			candidates = append(candidates, &stack.Containers[i])
		}
	}

	var selected []string
	for _, c := range candidates {
		if !containerMatchesLabels(c, sels) {
			continue
		}
		selected = append(selected, c.Name)
	}
	selected = append(selected, unknown...)
	sort.Strings(selected)

	if len(selected) == 0 {
		return nil, true, fmt.Errorf("no containers match the given name/label filters")
	}
	return selected, true, nil
}
