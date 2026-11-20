//go:build integration

package resolver

import (
	"fmt"
	"math/rand"
	"regexp"
	"testing"
	"time"
)

// Stress: determinism. The same configuration must resolve byte-identically on every
// evaluation, or the propagator rewrites the replicated Policy and the controller re-evaluates on
// every hub template refresh (flapping).

// nowRe matches a UTC timestamp as the managed cluster writes it.
var nowRe = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z`)

// maskNow replaces each timestamp within ten minutes of now with <now>. Namespace deletion records
// the managed cluster's evaluation time (onboarding-created-at of a namespace about to be created,
// a pending-deletion mark), so two evaluations a second apart differ there and only there. The hub
// output takes no time and stays byte-identical.
func maskNow(s string) string {
	now := time.Now()
	return nowRe.ReplaceAllStringFunc(s, func(m string) string {
		ts, err := time.Parse(time.RFC3339, m)
		if err == nil && ts.After(now.Add(-10*time.Minute)) && ts.Before(now.Add(10*time.Minute)) {
			return "<now>"
		}
		return m
	})
}

func stressDeterminismCfg(order []int) func(cfg map[string]interface{}) {
	return func(cfg map[string]interface{}) {
		stressAddAB(cfg)
		p := stressProjects(cfg)
		for _, i := range order {
			p[fmt.Sprintf("det-%02d", i)] = stressRealistic(i)
		}
		stressDefaults(cfg)["adminNetworkPolicies"] = stressAdminEntries(5)
	}
}

func TestStress_Determinism_SameConfig(t *testing.T) {
	t.Parallel()
	order := make([]int, 20)
	for i := range order {
		order[i] = i
	}
	var hub0, spoke0 string
	for run := 0; run < 4; run++ {
		o := append([]int(nil), order...)
		if run > 0 {
			rand.New(rand.NewSource(int64(run))).Shuffle(len(o), func(i, j int) { o[i], o[j] = o[j], o[i] })
		}
		r := stressRun(t, stressOpts{stubs: []string{stressPruneStubs}, labels: pruneOn, mutate: stressDeterminismCfg(o)})
		if run == 0 {
			hub0, spoke0 = r.hub, r.spoke
			continue
		}
		if r.hub != hub0 {
			t.Errorf("HIGH: run %d: hub-resolved Policy differs (%d vs %d bytes)", run, len(r.hub), len(hub0))
		}
		if maskNow(r.spoke) != maskNow(spoke0) {
			t.Errorf("HIGH: run %d: spoke-resolved Policy differs (%d vs %d bytes)", run, len(r.spoke), len(spoke0))
		}
	}
}

// The live objects come back from the API server in a stable order; the harness seeds them in file
// order. Reversing the seed must not change the hub output (it reads only the rendered config);
// the spoke output is checked for a stable sweep regardless of list order.
func TestStress_Determinism_LiveListOrder(t *testing.T) {
	t.Parallel()
	base := stressRun(t, stressOpts{stubs: []string{stressPruneStubs}, labels: pruneOn, mutate: stressDeterminismCfg([]int{0, 1, 2})})
	rev := stressRun(t, stressOpts{stubs: []string{stressPruneStubs}, labels: pruneOn, mutate: stressDeterminismCfg([]int{0, 1, 2}), reverseSeed: true})
	if base.hub != rev.hub {
		t.Errorf("hub output depends on live object order")
	}
	if base.spoke != rev.spoke {
		a, b := stressPruned(base.objs), stressPruned(rev.objs)
		if d := stressDiff(a, b); d != "" {
			t.Errorf("prune set depends on live object order:\n%s", d)
		} else {
			t.Logf("LOW: spoke output order follows the live list order (same set); stable on a real API server, which lists in key order")
		}
	}
}
