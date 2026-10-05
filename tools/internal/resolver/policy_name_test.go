package resolver

import (
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

	type violation struct {
		name, file string
	}
	var found []violation
	checked := 0

	err := filepath.Walk(filepath.Join(root, "policies"), func(path string, info os.FileInfo, err error) error {
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

	for _, v := range found {
		t.Errorf("policy name %q is %d characters (max %d) in %s\n"+
			"\t       With a 20-character policy namespace this exceeds the 62-character limit on the\n"+
			"\t       replicated policy name, and the policy never reaches the managed cluster.",
			v.name, len(v.name), maxPolicyNameLen, v.file)
	}
	t.Logf("policy names: %d checked, %d over %d characters", checked, len(found), maxPolicyNameLen)
}
