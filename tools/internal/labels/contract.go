package labels

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// BuildReport reconciles the consumed and declared label maps and returns a
// sorted contract report.
//
// Classification rules:
//   - OK: key is consumed by at least one policy AND declared in an _example*.yaml file.
//   - Missing: key is consumed but absent from all _example*.yaml files (contract violation).
//     A key declared only in non-example profile files (hub.yaml, etc.) does NOT satisfy
//     the contract — it is still Missing if consumed.
//   - Orphaned: key is declared in an _example*.yaml file but not consumed by any policy.
//     These are warnings (not contract violations unless --strict-orphans is set).
//   - Profile-only, not consumed: silently ignored (not Orphaned).
//
// The allow allowlist promotes specific Missing or Orphaned entries to OK.
// Passing nil is equivalent to passing an empty Allowlist.
func BuildReport(consumed map[string]*Consumed, declared map[string]*Declared, allow *Allowlist) Report {
	if allow == nil {
		allow = &Allowlist{}
	}

	// Union of all keys from both maps.
	allKeys := make(map[string]bool, len(consumed)+len(declared))
	for k := range consumed {
		allKeys[k] = true
	}
	for k := range declared {
		allKeys[k] = true
	}

	keys := make([]string, 0, len(allKeys))
	for k := range allKeys {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var report Report

	for _, key := range keys {
		c := consumed[key]
		d := declared[key]

		// A profile-only declaration (no example file) does not count as declared
		// for contract purposes.
		inExamples := d != nil && d.InExamples()

		var status string
		switch {
		case c != nil && inExamples:
			status = "ok"
		case c != nil && !inExamples:
			// Consumed but not declared in any example file — contract violation.
			status = "missing"
		case c == nil && inExamples:
			// In examples but no policy consumes it.
			status = "orphaned"
		default:
			// Profile-only declaration, not consumed → silently ignore.
			continue
		}

		// Apply allowlist promotions.
		if status == "missing" && allow.MissingOK[key] {
			status = "ok"
		}
		if status == "orphaned" && allow.OrphanedOK[key] {
			status = "ok"
		}

		entry := Entry{
			Key:      key,
			Declared: d,
			Consumed: c,
			Status:   status,
		}

		report.Entries = append(report.Entries, entry)
		switch status {
		case "ok":
			report.OK = append(report.OK, entry)
		case "missing":
			report.Missing = append(report.Missing, entry)
		case "orphaned":
			report.Orphaned = append(report.Orphaned, entry)
		}
	}

	return report
}

// LoadAllowlist reads an allowlist YAML file and returns an *Allowlist.
// The expected file format is:
//
//	missing_ok:
//	  - label-key-one
//	  - label-key-two
//	orphaned_ok:
//	  - other-label
//
// Returns an error if the file cannot be read or parsed.
func LoadAllowlist(path string) (*Allowlist, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read allowlist %s: %w", path, err)
	}

	var raw struct {
		MissingOK  []string `yaml:"missing_ok"`
		OrphanedOK []string `yaml:"orphaned_ok"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse allowlist %s: %w", path, err)
	}

	allow := &Allowlist{
		MissingOK:  make(map[string]bool, len(raw.MissingOK)),
		OrphanedOK: make(map[string]bool, len(raw.OrphanedOK)),
	}
	for _, k := range raw.MissingOK {
		allow.MissingOK[k] = true
	}
	for _, k := range raw.OrphanedOK {
		allow.OrphanedOK[k] = true
	}

	return allow, nil
}

// WriteMarkdown writes a Markdown-formatted label contract report to w.
func WriteMarkdown(w io.Writer, report Report) {
	fmt.Fprintf(w, "# Label Contract Report\n\n")

	buckets := []struct {
		heading string
		entries []Entry
	}{
		{"OK", report.OK},
		{"Missing — consumed by policies but absent from `_example*.yaml`", report.Missing},
		{"Orphaned — declared in `_example*.yaml` but not consumed by any policy", report.Orphaned},
	}

	for _, bucket := range buckets {
		fmt.Fprintf(w, "## %s (%d)\n\n", bucket.heading, len(bucket.entries))
		if len(bucket.entries) == 0 {
			fmt.Fprintf(w, "_none_\n\n")
			continue
		}
		fmt.Fprintf(w, "| Key | Policies |\n|-----|----------|\n")
		for _, entry := range bucket.entries {
			policies := ""
			if entry.Consumed != nil {
				policies = strings.Join(entry.Consumed.Policies(), ", ")
			}
			fmt.Fprintf(w, "| `%s` | %s |\n", entry.Key, policies)
		}
		fmt.Fprintf(w, "\n")
	}
}
