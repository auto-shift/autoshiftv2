package resolver

import (
	"github.com/auto-shift/autoshiftv2/tools/internal/labels"
)

// BuildSyntheticLabels converts the declared label catalog into a
// ManagedClusterLabels map suitable for hub template resolution.
//
// Each key gets the `autoshift.io/` prefix (as it would appear on a real
// ManagedCluster object). The value comes from the first example-file
// declaration that has a non-empty value. If no value is available, the key
// is still included with an empty string — this lets the resolver execute
// the template (hitting the `| default` path) rather than producing a
// Go template error for a missing map key.
func BuildSyntheticLabels(declared map[string]*labels.Declared) map[string]string {
	m := make(map[string]string, len(declared))

	for key, d := range declared {
		val := ""
		for _, decl := range d.Declarations {
			if decl.FromExample && decl.Value != "" {
				val = decl.Value
				break
			}
		}
		// Fall back to any declaration's value if no example has one.
		if val == "" {
			for _, decl := range d.Declarations {
				if decl.Value != "" {
					val = decl.Value
					break
				}
			}
		}
		m["autoshift.io/"+key] = val
	}

	return m
}
