package resolver

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// maxPolicyNameLen is the longest a Policy name may be. Combined with a 20-character policy
// namespace this reaches Red Hat Advanced Cluster Management's 62-character limit for the
// replicated policy name on a managed cluster.
const maxPolicyNameLen = 40

var (
	// A `- name:` entry directly under `policies:` in a policy-generator-config.yaml. The
	// two-space indent is what distinguishes it from a name nested inside a manifest entry.
	pgPolicyName = regexp.MustCompile(`(?m)^  - name: (.+)$`)
	// The four Helm holdouts still author the name themselves.
	helmPolicyName = regexp.MustCompile(`\$policyName := "([^"]+)"`)
)

// TestPolicyNameLengths fails a Policy name that cannot survive replication.
func TestPolicyNameLengths(t *testing.T) {
	root := repoRoot(t)
	findLongPolicyNames(t, root, filepath.Join(root, "policies"), true)
}

// findLongPolicyNames reports every Policy name over the limit under policiesDir. strict also
// fails a PolicyGenerator config that yielded no names at all, which means the pattern stopped
// matching rather than that the file is clean.
func findLongPolicyNames(t *testing.T, root, policiesDir string, strict bool) []string {
	t.Helper()

	type violation struct {
		name, file string
	}
	var found []violation
	checked := 0
	// Per-file match counts, so a pattern that stops matching fails loudly instead of
	// reporting success over zero names.
	perFile := map[string]int{}

	err := filepath.Walk(policiesDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".yaml") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}

		patterns := []*regexp.Regexp{helmPolicyName}
		if filepath.Base(path) == "policy-generator-config.yaml" {
			patterns = append(patterns, pgPolicyName)
		}
		for _, re := range patterns {
			for _, m := range re.FindAllStringSubmatch(string(body), -1) {
				name := strings.TrimSpace(m[1])
				checked++
				perFile[rel]++
				if len(name) > maxPolicyNameLen {
					found = append(found, violation{name, rel})
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk policies: %v", err)
	}

	msgs := make([]string, 0, len(found))
	for _, v := range found {
		msgs = append(msgs, fmt.Sprintf("policy name %q is %d characters (max %d) in %s",
			v.name, len(v.name), maxPolicyNameLen, v.file))
	}
	if !strict {
		return msgs
	}
	for _, v := range found {
		t.Errorf("policy name %q is %d characters (max %d) in %s\n"+
			"\t       With a 20-character policy namespace this exceeds the 62-character limit on the\n"+
			"\t       replicated policy name, and the policy never reaches the managed cluster.",
			v.name, len(v.name), maxPolicyNameLen, v.file)
	}
	// Every PolicyGenerator config declares at least one policy, so a config that yielded no
	// names means the pattern no longer matches the file rather than that the file is clean.
	helmNames := 0
	for file, n := range perFile {
		if strings.HasSuffix(file, "policy-generator-config.yaml") {
			continue
		}
		helmNames += n
	}
	err = filepath.Walk(policiesDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Base(path) != "policy-generator-config.yaml" {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		if perFile[rel] == 0 {
			t.Errorf("%s: matched no policy names, so this check is not looking at anything.\n"+
				"\t       Either the `  - name:` indentation changed or the file moved. Fix the\n"+
				"\t       pattern in pgPolicyName rather than leaving the check vacuous.", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk policies: %v", err)
	}
	if helmNames == 0 {
		t.Errorf(`no $policyName assignments found in any chart, so the Helm holdouts are unchecked.
	       Either the idiom changed or the holdouts are gone. Fix helmPolicyName.`)
	}

	t.Logf("policy names: %d checked across %d files, %d over %d characters",
		checked, len(perFile), len(found), maxPolicyNameLen)

	return msgs
}
