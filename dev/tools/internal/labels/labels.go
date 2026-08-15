// Package labels provides types and utilities for the AutoShift label contract:
// the relationship between labels declared in _example*.yaml files and labels
// consumed by policy Helm chart templates.
package labels

import "sort"

// Declaration represents a single label key found in a values file.
type Declaration struct {
	Key         string
	Value       string
	Path        string // YAML path to the containing labels block, e.g. "hubClusterSets.hub.labels"
	File        string // basename of the source file, e.g. "_example.yaml"
	FromExample bool   // true when File starts with "_example"
}

// Declared aggregates all declarations of one label key across all scanned files.
type Declared struct {
	Key          string
	Declarations []Declaration
}

// InExamples reports whether at least one declaration came from an _example*.yaml file.
func (d *Declared) InExamples() bool {
	for _, dec := range d.Declarations {
		if dec.FromExample {
			return true
		}
	}
	return false
}

// Reference records one occurrence of a label key in a rendered policy template.
type Reference struct {
	Key    string
	Policy string // policy path, e.g. "stable/cert-manager"
}

// Consumed aggregates all references to one label key across all policy charts.
type Consumed struct {
	Key        string
	References []Reference
}

// Policies returns a deduplicated, sorted list of policy paths that reference
// this label key.
func (c *Consumed) Policies() []string {
	seen := make(map[string]bool, len(c.References))
	var out []string
	for _, ref := range c.References {
		if !seen[ref.Policy] {
			seen[ref.Policy] = true
			out = append(out, ref.Policy)
		}
	}
	sort.Strings(out)
	return out
}

// Allowlist holds label keys that are exempt from specific contract violations.
type Allowlist struct {
	// MissingOK: consumed but undeclared keys that are intentionally exempt.
	MissingOK map[string]bool
	// OrphanedOK: declared-but-unconsumed keys that are intentionally exempt.
	OrphanedOK map[string]bool
}

// Entry is one row in a contract report.
type Entry struct {
	Key      string
	Declared *Declared
	Consumed *Consumed
	Status   string // "ok", "missing", or "orphaned"
}

// Report is the full label contract report produced by BuildReport.
type Report struct {
	// OK: keys that satisfy the contract (declared in examples AND consumed).
	OK []Entry
	// Missing: keys consumed by policies but absent from all _example*.yaml files.
	// These are contract violations that fail CI.
	Missing []Entry
	// Orphaned: keys declared in _example*.yaml files but not consumed by any policy.
	// These are warnings (fail CI only with --strict-orphans).
	Orphaned []Entry
	// Entries: all report entries sorted alphabetically by key.
	Entries []Entry
}
