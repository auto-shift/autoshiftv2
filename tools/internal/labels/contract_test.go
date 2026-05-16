package labels

import (
	"testing"
)

func TestBuildReport_AllBuckets(t *testing.T) {
	consumed := map[string]*Consumed{
		"ok-key": {
			Key: "ok-key",
			References: []Reference{
				{Key: "ok-key", Policy: "stable/a"},
			},
		},
		"missing-key": {
			Key: "missing-key",
		},
	}
	declared := map[string]*Declared{
		"ok-key": {
			Key: "ok-key",
			Declarations: []Declaration{
				{Key: "ok-key", File: "_example.yaml", FromExample: true},
			},
		},
		"orphaned-key": {
			Key: "orphaned-key",
			Declarations: []Declaration{
				{Key: "orphaned-key", File: "_example.yaml", FromExample: true},
			},
		},
	}

	rep := BuildReport(consumed, declared, nil)

	if len(rep.OK) != 1 || rep.OK[0].Key != "ok-key" {
		t.Errorf("OK bucket: %+v", rep.OK)
	}
	if len(rep.Missing) != 1 || rep.Missing[0].Key != "missing-key" {
		t.Errorf("Missing bucket: %+v", rep.Missing)
	}
	if len(rep.Orphaned) != 1 || rep.Orphaned[0].Key != "orphaned-key" {
		t.Errorf("Orphaned bucket: %+v", rep.Orphaned)
	}
}

func TestBuildReport_AllowlistDowngrades(t *testing.T) {
	consumed := map[string]*Consumed{
		"exempt-missing": {Key: "exempt-missing"},
		"normal-missing": {Key: "normal-missing"},
	}
	declared := map[string]*Declared{
		"exempt-orphan": {
			Key: "exempt-orphan",
			Declarations: []Declaration{
				{Key: "exempt-orphan", File: "_example.yaml", FromExample: true},
			},
		},
	}
	allow := &Allowlist{
		MissingOK:  map[string]bool{"exempt-missing": true},
		OrphanedOK: map[string]bool{"exempt-orphan": true},
	}

	rep := BuildReport(consumed, declared, allow)

	okKeys := map[string]bool{}
	for _, e := range rep.OK {
		okKeys[e.Key] = true
	}
	if !okKeys["exempt-missing"] {
		t.Errorf("exempt-missing should be OK via allowlist")
	}
	if !okKeys["exempt-orphan"] {
		t.Errorf("exempt-orphan should be OK via allowlist")
	}
	if len(rep.Missing) != 1 || rep.Missing[0].Key != "normal-missing" {
		t.Errorf("normal-missing: %+v", rep.Missing)
	}
	if len(rep.Orphaned) != 0 {
		t.Errorf("Orphaned should be empty: %+v", rep.Orphaned)
	}
}

func TestBuildReport_ProfileOnlyDeclarationIsIgnored(t *testing.T) {
	declared := map[string]*Declared{
		"profile-only": {
			Key: "profile-only",
			Declarations: []Declaration{
				{Key: "profile-only", File: "hub.yaml", FromExample: false},
			},
		},
	}
	rep := BuildReport(map[string]*Consumed{}, declared, nil)
	if len(rep.Orphaned) != 0 {
		t.Errorf("profile-only should not be orphaned: %+v", rep.Orphaned)
	}
}

func TestBuildReport_ProfileDeclarationDoesNotSatisfyContract(t *testing.T) {
	consumed := map[string]*Consumed{
		"needs-catalog": {Key: "needs-catalog"},
	}
	declared := map[string]*Declared{
		"needs-catalog": {
			Key: "needs-catalog",
			Declarations: []Declaration{
				{Key: "needs-catalog", File: "hub.yaml", FromExample: false},
			},
		},
	}
	rep := BuildReport(consumed, declared, nil)
	if len(rep.Missing) != 1 || rep.Missing[0].Key != "needs-catalog" {
		t.Errorf("needs-catalog should be missing: %+v", rep)
	}
}

func TestBuildReport_StableOrder(t *testing.T) {
	consumed := map[string]*Consumed{
		"z": {Key: "z"},
		"a": {Key: "a"},
		"m": {Key: "m"},
	}
	declared := map[string]*Declared{
		"a": {Key: "a", Declarations: []Declaration{{Key: "a", FromExample: true}}},
		"m": {Key: "m", Declarations: []Declaration{{Key: "m", FromExample: true}}},
		"z": {Key: "z", Declarations: []Declaration{{Key: "z", FromExample: true}}},
	}
	rep := BuildReport(consumed, declared, nil)
	if len(rep.Entries) != 3 {
		t.Fatalf("want 3 entries, got %d", len(rep.Entries))
	}
	if rep.Entries[0].Key != "a" || rep.Entries[1].Key != "m" || rep.Entries[2].Key != "z" {
		t.Errorf("not sorted: %v %v %v", rep.Entries[0].Key, rep.Entries[1].Key, rep.Entries[2].Key)
	}
}
