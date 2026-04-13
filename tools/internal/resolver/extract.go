package resolver

import (
	"sort"

	"github.com/auto-shift/autoshiftv2/tools/internal/labels"
)

// KeysToConsumed converts a set of consumed key names and a policy attribution
// string into the labels.Consumed map expected by the contract checker.
func KeysToConsumed(keysByPolicy map[string]map[string]bool) map[string]*labels.Consumed {
	all := map[string]*labels.Consumed{}

	for policy, keys := range keysByPolicy {
		for key := range keys {
			c, ok := all[key]
			if !ok {
				c = &labels.Consumed{Key: key}
				all[key] = c
			}
			c.References = append(c.References, labels.Reference{
				Key:    key,
				Policy: policy,
			})
		}
	}

	return all
}

// SortedKeys returns sorted keys from a bool map, useful for deterministic output.
func SortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
