//go:build integration

package resolver

import (
	"os"
	"sort"
	"testing"
)

// TestStressProbe dumps the prune and render sets for the stress-a/stress-b fixture. Debug aid only.
func TestStressProbe(t *testing.T) {
	if os.Getenv("STRESS_PROBE") == "" {
		t.Skip("set STRESS_PROBE=1 to dump the fixture")
	}
	r := stressRun(t, stressOpts{stubs: []string{"stress-onboarding-prune.yaml"}, labels: pruneOn, mutate: stressAddAB})
	p := stressPruned(r.objs)
	var keys []string
	for k, v := range p {
		keys = append(keys, k+" -> "+v)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t.Log("PRUNE ", k)
	}
	keys = nil
	for k, v := range stressRendered(r.objs) {
		keys = append(keys, k+" -> "+v)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t.Log("RENDER", k)
	}
	t.Log(stressStatus(t, r.objs))
	t.Log(r.hubDur, r.spokeDur)
}
