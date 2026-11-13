//go:build integration

package resolver

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"
)

// This file holds the checking half of the scenario suite (scenario_test.go): it turns resolved
// policy output into the objects a cluster would receive, and compares the scenario's expected
// objects with them. Comparison is on parsed objects rather than text, so it survives reformatting
// and can tell a missing key from an empty one.

// renderedObject is one object a resolved chart would put on a cluster, or the policy wrapper
// around it.
type renderedObject struct {
	Chart  string // stable/quay
	Policy string // metadata.name of the enclosing Policy; the Policy's own name for a Policy
	Object map[string]any
}

func (o renderedObject) kind() string { s, _ := o.Object["kind"].(string); return s }
func (o renderedObject) name() string {
	s, _ := nested(o.Object, "metadata", "name").(string)
	return s
}
func (o renderedObject) namespace() string {
	s, _ := nested(o.Object, "metadata", "namespace").(string)
	return s
}

func (o renderedObject) String() string {
	id := o.kind() + "/" + o.name()
	if ns := o.namespace(); ns != "" {
		id = o.kind() + "/" + ns + "/" + o.name()
	}
	return fmt.Sprintf("%s in %s (%s)", id, o.Policy, o.Chart)
}

// collectRenderedObjects walks a chart's resolved stream: every top-level document, every policy
// template inside a Policy, and every object inside a ConfigurationPolicy's object-templates or
// object-templates-raw. A raw block still holding template markers did not resolve, so its objects
// are reported in unresolved rather than guessed at. A resolved raw block that does not parse is
// returned in errs: its objects were never collected, so an expected object could look absent.
func collectRenderedObjects(chart, resolved string) (objs []renderedObject, unresolved, errs []string) {
	dec := yaml.NewDecoder(strings.NewReader(resolved))
	for {
		var doc map[string]any
		if err := dec.Decode(&doc); err != nil {
			break // end of stream, or a document the pipeline checks already report
		}
		if doc == nil {
			continue
		}
		policyName, _ := nested(doc, "metadata", "name").(string)
		objs = append(objs, renderedObject{chart, policyName, doc})
		if doc["kind"] != "Policy" {
			continue
		}
		templates, _ := nested(doc, "spec", "policy-templates").([]any)
		for _, pt := range templates {
			od, _ := nested(asMap(pt), "objectDefinition").(map[string]any)
			if od == nil {
				continue
			}
			objs = append(objs, renderedObject{chart, policyName, od})
			spec := asMap(od["spec"])
			if raw, ok := spec["object-templates-raw"].(string); ok {
				if strings.Contains(raw, "{{") {
					unresolved = append(unresolved, policyName)
					continue
				}
				var items []any
				if err := yaml.Unmarshal([]byte(raw), &items); err != nil {
					errs = append(errs, fmt.Sprintf("%s: object-templates-raw does not parse as YAML: %v", policyName, err))
					continue
				}
				objs = appendTemplateObjects(objs, chart, policyName, items)
			}
			if list, ok := spec["object-templates"].([]any); ok {
				objs = appendTemplateObjects(objs, chart, policyName, list)
			}
		}
	}
	return objs, unresolved, errs
}

func appendTemplateObjects(objs []renderedObject, chart, policy string, items []any) []renderedObject {
	for _, it := range items {
		if od, ok := asMap(it)["objectDefinition"].(map[string]any); ok {
			objs = append(objs, renderedObject{chart, policy, decodeSecretData(od)})
		}
	}
	return objs
}

// decodeSecretData returns a Secret with its data values base64-decoded, as `oc extract` shows them,
// so an expected object compares data values as text. A value that is not valid base64 is left as it is,
// and any other object is returned unchanged.
func decodeSecretData(od map[string]any) map[string]any {
	data, ok := od["data"].(map[string]any)
	if od["kind"] != "Secret" || !ok {
		return od
	}
	decoded := make(map[string]any, len(data))
	for k, v := range data {
		decoded[k] = v
		if s, ok := v.(string); ok {
			if b, err := base64.StdEncoding.DecodeString(s); err == nil {
				decoded[k] = string(b)
			}
		}
	}
	out := make(map[string]any, len(od))
	for k, v := range od {
		out[k] = v
	}
	out["data"] = decoded
	return out
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

// expectedObject is one document from a scenario's expected/ directory: part of an object the
// policies must render. A file directly under expected/ matches an object from any policy; a file
// under expected/<policy-name>/ matches only objects that Policy applies.
type expectedObject struct {
	File   string         // path relative to the scenario, for messages
	Policy string         // enclosing Policy name; empty matches any
	Object map[string]any // the partial object
}

func (e expectedObject) kind() string { s, _ := e.Object["kind"].(string); return s }
func (e expectedObject) name() string {
	s, _ := nested(e.Object, "metadata", "name").(string)
	return s
}
func (e expectedObject) namespace() string {
	s, _ := nested(e.Object, "metadata", "namespace").(string)
	return s
}

func (e expectedObject) String() string {
	id := e.kind() + "/" + e.name()
	if ns := e.namespace(); ns != "" {
		id = e.kind() + "/" + ns + "/" + e.name()
	}
	return fmt.Sprintf("%s (%s)", id, e.File)
}

func (e expectedObject) selects(o renderedObject) bool {
	return o.kind() == e.kind() && o.name() == e.name() &&
		(e.namespace() == "" || o.namespace() == e.namespace()) &&
		(e.Policy == "" || o.Policy == e.Policy)
}

// loadExpected reads the objects the deployment must render, from dir/expected. It is required.
func loadExpected(t *testing.T, dir string) []expectedObject {
	t.Helper()
	out, err := loadObjectDir(dir, "expected")
	if errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("%s: no expected/ directory; say what the deployment must render", dir)
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatalf("%s: expected/ holds no objects; say what the deployment must render", dir)
	}
	return out
}

// loadAbsent reads the objects the deployment must not render, from dir/absent. It is optional.
func loadAbsent(t *testing.T, dir string) []expectedObject {
	t.Helper()
	out, err := loadObjectDir(dir, "absent")
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// loadObjectDir reads every YAML document under dir/sub, which holds files or one level of
// <policy-name>/ directories. Each document needs a kind and metadata.name, because those select
// the rendered objects it is compared with.
func loadObjectDir(dir, sub string) ([]expectedObject, error) {
	root := filepath.Join(dir, sub)
	var out []expectedObject
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if rel, _ := filepath.Rel(root, path); strings.Count(rel, string(filepath.Separator)) > 0 {
				return fmt.Errorf("%s: %s/ holds files or one level of <policy-name>/ directories", path, sub)
			}
			return nil
		}
		if ext := filepath.Ext(path); ext != ".yaml" && ext != ".yml" {
			return fmt.Errorf("%s: %s/ holds only .yaml files", path, sub)
		}
		rel, _ := filepath.Rel(dir, path)
		policy := ""
		if sub := filepath.Dir(path); sub != root {
			policy = filepath.Base(sub)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		dec := yaml.NewDecoder(bytes.NewReader(data))
		for {
			var doc map[string]any
			if err := dec.Decode(&doc); err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				return fmt.Errorf("%s: %w", rel, err)
			}
			if doc == nil {
				continue
			}
			e := expectedObject{File: rel, Policy: policy, Object: doc}
			if e.kind() == "" || e.name() == "" {
				return fmt.Errorf("%s: every object needs kind and metadata.name, which select the rendered object", rel)
			}
			out = append(out, e)
		}
		return nil
	})
	return out, err
}

// subsetMismatches compares a partial expected value with a rendered one, the way an ACM musthave
// object template compares with a live object, and returns every difference. Maps match by key and
// lists of objects by any entry that contains the expected one; scalars must be equal, including
// type. Two additions for tests: a null expected value asserts the key is absent, and an expected
// map compared with a string parses the string as YAML, so an embedded config file such as a
// Secret's config.yaml can be checked by key.
func subsetMismatches(want, got any, path string) []string {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if s, isStr := got.(string); isStr {
			var parsed map[string]any
			if err := yaml.Unmarshal([]byte(s), &parsed); err == nil && parsed != nil {
				g, ok = parsed, true
			}
		}
		if !ok {
			return []string{fmt.Sprintf("%s: expected a map, got %s", path, brief(got))}
		}
		var out []string
		for _, k := range sortedKeys(w) {
			kp := path + "." + k
			if path == "" {
				kp = k
			}
			gv, has := g[k]
			if w[k] == nil {
				if has && gv != nil {
					out = append(out, fmt.Sprintf("%s: expected absent, found %s", kp, brief(gv)))
				}
				continue
			}
			if !has {
				out = append(out, fmt.Sprintf("%s: missing (expected %s)", kp, brief(w[k])))
				continue
			}
			out = append(out, subsetMismatches(w[k], gv, kp)...)
		}
		return out
	case []any:
		g, ok := got.([]any)
		if !ok {
			return []string{fmt.Sprintf("%s: expected a list, got %s", path, brief(got))}
		}
		var out []string
		for i, wi := range w {
			// Report how the closest entry differs, rather than every entry in the list.
			var closest []string
			found := false
			for _, gi := range g {
				diffs := subsetMismatches(wi, gi, "")
				if len(diffs) == 0 {
					found = true
					break
				}
				if closest == nil || len(diffs) < len(closest) {
					closest = diffs
				}
			}
			if found {
				continue
			}
			if closest == nil {
				out = append(out, fmt.Sprintf("%s[%d]: the list is empty", path, i))
				continue
			}
			out = append(out, fmt.Sprintf("%s[%d]: no entry matches; closest: %s", path, i, strings.Join(closest, "; ")))
		}
		return out
	default:
		if !reflect.DeepEqual(want, got) {
			msg := fmt.Sprintf("expected %s (%T), got %s (%T)", brief(want), want, brief(got), got)
			if path != "" {
				msg = path + ": " + msg
			}
			return []string{msg}
		}
		return nil
	}
}

func brief(v any) string {
	b, err := yaml.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		s = s[:300] + "..."
	}
	return s
}

// evaluateExpected checks every expected object against the scenario's rendered objects. At least
// one rendered object must be selected, and every selected one must contain the expected object.
// It returns the charts an expected object was found in, which are the charts the scenario is about.
func evaluateExpected(t *testing.T, expected []expectedObject, objs []renderedObject, unresolved []string) map[string]bool {
	t.Helper()
	charts := map[string]bool{}
	for _, e := range expected {
		var matched []renderedObject
		for _, o := range objs {
			if e.selects(o) {
				matched = append(matched, o)
				charts[o.Chart] = true
			}
		}
		if len(matched) == 0 {
			t.Errorf("expected %s: no policy renders it%s", e, candidates(e, objs, unresolved))
			continue
		}
		for _, o := range matched {
			if diffs := subsetMismatches(e.Object, o.Object, ""); len(diffs) > 0 {
				t.Errorf("expected %s differs from %s:\n  %s", e, o, strings.Join(diffs, "\n  "))
			}
		}
	}
	return charts
}

// evaluateAbsent fails for every rendered object an absent/ entry selects. Only kind, name,
// namespace and the policy directory select; any other field in the entry is ignored.
func evaluateAbsent(t *testing.T, absent []expectedObject, objs []renderedObject) {
	t.Helper()
	for _, e := range absent {
		for _, o := range objs {
			if e.selects(o) {
				t.Errorf("absent %s: rendered by %s", e, o)
			}
		}
	}
}

// candidates lists rendered objects of the same kind, so a renamed object or a policy that rendered
// nothing is diagnosable from the failure alone.
func candidates(e expectedObject, objs []renderedObject, unresolved []string) string {
	var lines []string
	for _, o := range objs {
		if o.kind() == e.kind() {
			lines = append(lines, "same kind  "+o.String())
		}
	}
	for _, p := range unresolved {
		lines = append(lines, "unresolved object-templates-raw in "+p)
	}
	if len(lines) == 0 {
		return " (no object of that kind rendered at all)"
	}
	sort.Strings(lines)
	return "\n  " + strings.Join(lines, "\n  ")
}

// unplacedPolicies returns the Policies whose PlacementBinding points at a Placement that does not
// select a cluster with clusterLabels. Every policy resolves in the suite whatever its placement,
// so without this a scenario would compare objects a real cluster never receives, such as the
// managed-mode CloudNativePG clusters in an external-mode deployment. A Policy no binding names is
// treated as placed. Placement predicates are ORed, and a Placement with none selects every cluster.
func unplacedPolicies(objs []renderedObject, clusterLabels map[string]string) (map[string]bool, error) {
	placements := map[string]map[string]any{}
	type binding struct{ placement, policy string }
	var bindings []binding
	for _, o := range objs {
		switch o.kind() {
		case "Placement":
			placements[o.name()] = o.Object
		case "PlacementBinding":
			ref, _ := nested(o.Object, "placementRef", "name").(string)
			subjects, _ := o.Object["subjects"].([]any)
			for _, sub := range subjects {
				m := asMap(sub)
				if m["kind"] == "Policy" {
					name, _ := m["name"].(string)
					bindings = append(bindings, binding{ref, name})
				}
			}
		}
	}
	set := k8slabels.Set(clusterLabels)
	selected := map[string]bool{}
	for name, pl := range placements {
		preds, _ := nested(pl, "spec", "predicates").([]any)
		ok := len(preds) == 0
		for _, pr := range preds {
			raw := nested(asMap(pr), "requiredClusterSelector", "labelSelector")
			b, err := json.Marshal(raw)
			if err != nil {
				return nil, err
			}
			var ls metav1.LabelSelector
			if err := json.Unmarshal(b, &ls); err != nil {
				return nil, fmt.Errorf("Placement %s: %w", name, err)
			}
			sel, err := metav1.LabelSelectorAsSelector(&ls)
			if err != nil {
				return nil, fmt.Errorf("Placement %s: %w", name, err)
			}
			if sel.Matches(set) {
				ok = true
				break
			}
		}
		selected[name] = ok
	}
	placed := map[string]bool{}
	bound := map[string]bool{}
	for _, b := range bindings {
		bound[b.policy] = true
		if selected[b.placement] {
			placed[b.policy] = true
		}
	}
	out := map[string]bool{}
	for p := range bound {
		if !placed[p] {
			out[p] = true
		}
	}
	return out, nil
}

// TestUnplacedPolicies checks predicate evaluation against cluster labels.
func TestUnplacedPolicies(t *testing.T) {
	var objs []renderedObject
	for _, doc := range []string{
		`{kind: Placement, metadata: {name: pl-managed}, spec: {predicates: [{requiredClusterSelector: {labelSelector: {matchExpressions: [{key: autoshift.io/quay-db-mode, operator: In, values: [managed]}]}}}]}}`,
		`{kind: Placement, metadata: {name: pl-quay}, spec: {predicates: [{requiredClusterSelector: {labelSelector: {matchExpressions: [{key: autoshift.io/quay, operator: In, values: ['true']}, {key: autoshift.io/quay-db-mode, operator: NotIn, values: [managed]}]}}}]}}`,
		`{kind: Placement, metadata: {name: pl-all}, spec: {}}`,
		`{kind: PlacementBinding, metadata: {name: b1}, placementRef: {name: pl-managed}, subjects: [{kind: Policy, name: policy-cnpg}]}`,
		`{kind: PlacementBinding, metadata: {name: b2}, placementRef: {name: pl-quay}, subjects: [{kind: Policy, name: policy-deploy}, {kind: Policy, name: policy-check}]}`,
		`{kind: PlacementBinding, metadata: {name: b3}, placementRef: {name: pl-all}, subjects: [{kind: Policy, name: policy-everywhere}]}`,
	} {
		var m map[string]any
		if err := yaml.Unmarshal([]byte(doc), &m); err != nil {
			t.Fatal(err)
		}
		objs = append(objs, renderedObject{"stable/test", "", m})
	}
	got, err := unplacedPolicies(objs, map[string]string{"autoshift.io/quay": "true", "autoshift.io/quay-db-mode": "external"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"policy-cnpg": true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("unplaced = %v; want %v", got, want)
	}
}

// TestSubsetMismatches covers the comparison rules expected/ files rely on.
func TestSubsetMismatches(t *testing.T) {
	var got map[string]any
	if err := yaml.Unmarshal([]byte(`
kind: QuayRegistry
spec:
  flag: false
  components:
    - {kind: postgres, managed: false}
    - {kind: redis, managed: true, overrides: {replicas: 2}}
data:
  config.yaml: "DB_URI: postgresql://db\nFEATURE_X: true\n"
`), &got); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, want string
		ok         bool
	}{
		{"scalar equal", "spec: {flag: false}", true},
		{"scalar type differs", "spec: {flag: 'false'}", false},
		{"list entry contained", "spec: {components: [{kind: postgres, managed: false}]}", true},
		{"list entry differs", "spec: {components: [{kind: postgres, managed: true}]}", false},
		{"empty map means exists", "spec: {components: [{kind: redis, overrides: {}}]}", true},
		{"null means absent", "spec: {missing: null}", true},
		{"null on a present key", "spec: {flag: null}", false},
		{"missing key", "spec: {other: 1}", false},
		{"embedded YAML by key", "data: {config.yaml: {DB_URI: postgresql://db}}", true},
		{"embedded YAML key differs", "data: {config.yaml: {DB_URI: postgresql://other}}", false},
		{"embedded YAML absent key", "data: {config.yaml: {SSL: null}}", true},
	}
	for _, c := range cases {
		var want map[string]any
		if err := yaml.Unmarshal([]byte(c.want), &want); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		diffs := subsetMismatches(want, got, "")
		if (len(diffs) == 0) != c.ok {
			t.Errorf("%s: mismatches %v; want ok=%v", c.name, diffs, c.ok)
		}
	}
}

// TestCollectRenderedObjects covers the collector's two transformations: a Secret's data is
// decoded for checks, and a resolved raw block that does not parse is an error, not skipped.
func TestCollectRenderedObjects(t *testing.T) {
	resolved := `apiVersion: policy.open-cluster-management.io/v1
kind: Policy
metadata:
  name: policy-good
spec:
  policy-templates:
    - objectDefinition:
        apiVersion: policy.open-cluster-management.io/v1
        kind: ConfigurationPolicy
        metadata:
          name: good
        spec:
          object-templates-raw: |
            - complianceType: mustonlyhave
              objectDefinition:
                apiVersion: v1
                kind: Secret
                metadata:
                  name: bundle
                data:
                  config.yaml: REJfVVJJOiB4
                  raw: not-base64!
---
apiVersion: policy.open-cluster-management.io/v1
kind: Policy
metadata:
  name: policy-bad
spec:
  policy-templates:
    - objectDefinition:
        apiVersion: policy.open-cluster-management.io/v1
        kind: ConfigurationPolicy
        metadata:
          name: bad
        spec:
          object-templates-raw: |
            - complianceType: musthave
              objectDefinition: {kind: Secret
`
	objs, unresolved, errs := collectRenderedObjects("stable/test", resolved)
	if len(unresolved) != 0 {
		t.Errorf("unresolved = %v; want none", unresolved)
	}
	if len(errs) != 1 || !strings.Contains(errs[0], "policy-bad") {
		t.Errorf("errs = %v; want one error naming policy-bad", errs)
	}
	var secret map[string]any
	for _, o := range objs {
		if o.kind() == "Secret" && o.name() == "bundle" {
			secret = o.Object
		}
	}
	if secret == nil {
		t.Fatal("Secret bundle was not collected")
	}
	data := asMap(secret["data"])
	if got := data["config.yaml"]; got != "DB_URI: x" {
		t.Errorf(`data["config.yaml"] = %q; want the decoded "DB_URI: x"`, got)
	}
	if got := data["raw"]; got != "not-base64!" {
		t.Errorf(`data["raw"] = %q; want the value left as it is`, got)
	}
}

// TestLoadScenarioValues_ClusterLabelWins checks the documented precedence: a cluster label
// overrides the same label set on its clusterset, whatever order the keys sort in.
func TestLoadScenarioValues_ClusterLabelWins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "values.yaml")
	values := `clusters:
  one:
    labels:
      quay-db-mode: external
hubClusterSets:
  hub:
    labels:
      quay-db-mode: managed
managedClusterSets:
  managed:
    labels:
      quay-db-mode: bundled
`
	if err := os.WriteFile(path, []byte(values), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := loadScenarioValues(t, path).labels["quay-db-mode"]; got != "external" {
		t.Errorf("quay-db-mode = %q; want the cluster's external", got)
	}
}
