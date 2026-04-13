package labels

import (
	"sort"
)

// Status classifies a label key under the label contract.
//
// The rule is simple: every `autoshift.io/<key>` referenced by a policy
// template must be documented in an `_example*.yaml` catalog file. No
// exceptions for `| default`, placement selectors, or otherwise.
type Status int

const (
	// StatusOK: the key is both documented in an example file and consumed
	// by at least one policy template (or covered by a prefix consumer).
	StatusOK Status = iota

	// StatusMissing: the key is consumed by a policy template but is NOT
	// declared in any `_example*.yaml` file. Fails CI.
	StatusMissing

	// StatusOrphaned: the key is declared in an example file but no policy
	// template consumes it. Usually dead documentation.
	StatusOrphaned
)

func (s Status) String() string {
	switch s {
	case StatusOK:
		return "ok"
	case StatusMissing:
		return "missing"
	case StatusOrphaned:
		return "orphaned"
	default:
		return "unknown"
	}
}

// Entry is a single line in the contract report: one label key and its status.
type Entry struct {
	Key      string
	Status   Status
	Consumed *Consumed // non-nil if the key is consumed by any template
	Declared *Declared // non-nil if the key is declared in any values file
}

// Report is the full contract analysis.
type Report struct {
	Entries []Entry

	// Quick-access buckets (references into Entries).
	OK       []Entry
	Missing  []Entry
	Orphaned []Entry
}

// Allowlist exempts specific keys from specific statuses. A key in
// OrphanedOK is allowed to be declared-but-unused (e.g. profile-level knobs
// the user might enable later). A key in MissingOK is allowed to be consumed
// but undeclared (e.g. advanced tuning labels intentionally left out of all
// profiles).
type Allowlist struct {
	// Keys that may be orphaned (declared-but-unused) without failing.
	OrphanedOK map[string]bool
	// Keys that may be consumed-but-undeclared (required or optional) without
	// failing.
	MissingOK map[string]bool
}

// BuildReport reconciles consumed and declared label maps into a Report.
// Pass a nil allowlist to get the raw picture.
//
// A key is considered "declared" iff it appears in at least one
// `_example*.yaml` file. The other values profiles (hub.yaml, managed.yaml,
// etc.) are curated recommended subsets and don't define the label catalog,
// so declarations from them don't count toward the contract check.
//
// Prefix-style consumed keys (where every reference is `hasPrefix`) match any
// declared key that begins with the prefix — even if only the example file
// contains the matching suffix.
func BuildReport(consumed map[string]*Consumed, declared map[string]*Declared, allow *Allowlist) Report {
	if allow == nil {
		allow = &Allowlist{}
	}

	// Union of all keys.
	keys := map[string]struct{}{}
	for k := range consumed {
		keys[k] = struct{}{}
	}
	for k := range declared {
		keys[k] = struct{}{}
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)

	var rep Report
	for _, k := range sorted {
		c := consumed[k]
		d := declared[k]

		// "Declared" means documented in an _example*.yaml file.
		// Profile-only declarations don't count.
		inCatalog := d != nil && d.InExamples()

		var status Status
		switch {
		case c != nil && inCatalog:
			status = StatusOK

		case c != nil && !inCatalog:
			if allow.MissingOK[k] {
				status = StatusOK
			} else {
				status = StatusMissing
			}

		case c == nil && inCatalog:
			if allow.OrphanedOK[k] {
				status = StatusOK
			} else {
				status = StatusOrphaned
			}

		case c == nil && !inCatalog:
			// Profile-only — silently ignored.
			status = StatusOK
		}

		entry := Entry{Key: k, Status: status, Consumed: c, Declared: d}
		rep.Entries = append(rep.Entries, entry)
		switch status {
		case StatusOK:
			rep.OK = append(rep.OK, entry)
		case StatusMissing:
			rep.Missing = append(rep.Missing, entry)
		case StatusOrphaned:
			rep.Orphaned = append(rep.Orphaned, entry)
		}
	}
	return rep
}
