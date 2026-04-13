package labels

import "sort"

// Reference records a single occurrence of an autoshift.io/<key> label
// consumed by a policy chart.
type Reference struct {
	Key    string // the label key without the `autoshift.io/` prefix
	Policy string // <category>/<chart> e.g. "stable/cert-manager"
}

// Consumed is the aggregate view of one label key across all its references.
type Consumed struct {
	Key        string
	References []Reference
}

// Policies returns the unique set of policies (category/chart) that reference
// this label, sorted.
func (c Consumed) Policies() []string {
	set := map[string]struct{}{}
	for _, r := range c.References {
		set[r.Policy] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// SortedConsumed returns Consumed values sorted by key.
func SortedConsumed(m map[string]*Consumed) []*Consumed {
	out := make([]*Consumed, 0, len(m))
	for _, c := range m {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}
