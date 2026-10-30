//go:build integration

package resolver

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// This file holds the assertion half of the scenario suite (scenario_test.go): it turns resolved
// policy output into the objects a cluster would receive, and checks scenario expectations against
// them. Assertions match parsed objects rather than text, so they survive reformatting and can tell
// a missing key from an empty one.

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
// are reported in unresolved rather than guessed at.
func collectRenderedObjects(chart, resolved string) (objs []renderedObject, unresolved []string) {
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
					continue // reported by TestObjectTemplatesRaw_ParsesAsYAML
				}
				objs = appendTemplateObjects(objs, chart, policyName, items)
			}
			if list, ok := spec["object-templates"].([]any); ok {
				objs = appendTemplateObjects(objs, chart, policyName, list)
			}
		}
	}
	return objs, unresolved
}

func appendTemplateObjects(objs []renderedObject, chart, policy string, items []any) []renderedObject {
	for _, it := range items {
		if od, ok := asMap(it)["objectDefinition"].(map[string]any); ok {
			objs = append(objs, renderedObject{chart, policy, od})
		}
	}
	return objs
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

// expectation selects rendered objects and checks fields on each of them.
type expectation struct {
	Policy    string       `yaml:"policy"`    // enclosing Policy name; empty matches any
	Kind      string       `yaml:"kind"`      // required
	Name      string       `yaml:"name"`      // empty matches any
	Namespace string       `yaml:"namespace"` // empty matches any
	Count     *int         `yaml:"count"`     // exact number of matches; omitted means at least one
	Fields    []fieldCheck `yaml:"fields"`    // checked on every match
}

func (e expectation) String() string {
	s := e.Kind
	if e.Namespace != "" {
		s += "/" + e.Namespace
	}
	if e.Name != "" {
		s += "/" + e.Name
	}
	if e.Policy != "" {
		s += " in " + e.Policy
	}
	return s
}

func (e expectation) selects(o renderedObject) bool {
	return o.kind() == e.Kind &&
		(e.Name == "" || o.name() == e.Name) &&
		(e.Namespace == "" || o.namespace() == e.Namespace) &&
		(e.Policy == "" || o.Policy == e.Policy)
}

// fieldCheck is one assertion on the value at Path. Exactly one operator is set.
type fieldCheck struct {
	Path string
	op   string // equals, contains, notContains, absent, present
	want any
}

var fieldOps = map[string]bool{"equals": true, "contains": true, "notContains": true, "absent": true, "present": true}

// UnmarshalYAML rejects unknown keys and a missing or doubled operator, so a typo such as
// `equal:` fails the scenario instead of asserting nothing.
func (f *fieldCheck) UnmarshalYAML(n *yaml.Node) error {
	var raw map[string]any
	if err := n.Decode(&raw); err != nil {
		return err
	}
	for k, v := range raw {
		switch {
		case k == "path":
			s, ok := v.(string)
			if !ok || s == "" {
				return fmt.Errorf("line %d: path must be a non-empty string", n.Line)
			}
			f.Path = s
		case fieldOps[k]:
			if f.op != "" {
				return fmt.Errorf("line %d: field check sets both %s and %s; use one per entry", n.Line, f.op, k)
			}
			f.op, f.want = k, v
		default:
			return fmt.Errorf("line %d: unknown field check key %q (want path plus one of equals, contains, notContains, absent, present)", n.Line, k)
		}
	}
	if f.Path == "" || f.op == "" {
		return fmt.Errorf("line %d: a field check needs path and one of equals, contains, notContains, absent, present", n.Line)
	}
	if f.op == "contains" || f.op == "notContains" {
		if _, ok := f.want.(string); !ok {
			return fmt.Errorf("line %d: %s takes a string", n.Line, f.op)
		}
	}
	if f.op == "absent" || f.op == "present" {
		if b, ok := f.want.(bool); !ok || !b {
			return fmt.Errorf("line %d: %s takes true", n.Line, f.op)
		}
	}
	if _, err := parsePath(f.Path); err != nil {
		return fmt.Errorf("line %d: %w", n.Line, err)
	}
	return nil
}

// check returns "" when the object satisfies the field check, otherwise why it does not.
func (f fieldCheck) check(obj map[string]any) string {
	steps, _ := parsePath(f.Path) // validated at load
	got, found, err := lookupPath(obj, steps)
	if err != nil {
		return fmt.Sprintf("%s: %v", f.Path, err)
	}
	switch f.op {
	case "absent":
		if found {
			return fmt.Sprintf("%s: expected absent, found %s", f.Path, brief(got))
		}
		return ""
	case "present":
		if !found {
			return fmt.Sprintf("%s: expected present, not found", f.Path)
		}
		return ""
	}
	if !found {
		return fmt.Sprintf("%s: not found (expected %s %s)", f.Path, f.op, brief(f.want))
	}
	switch f.op {
	case "equals":
		if !reflect.DeepEqual(got, f.want) {
			return fmt.Sprintf("%s: expected %s (%T), got %s (%T)", f.Path, brief(f.want), f.want, brief(got), got)
		}
	case "contains", "notContains":
		s, ok := got.(string)
		if !ok {
			return fmt.Sprintf("%s: %s needs a string, got %T", f.Path, f.op, got)
		}
		if has := strings.Contains(s, f.want.(string)); has != (f.op == "contains") {
			return fmt.Sprintf("%s: expected it to %s %q\n--- value ---\n%s", f.Path, map[bool]string{true: "contain", false: "not contain"}[f.op == "contains"], f.want, indent(s))
		}
	}
	return ""
}

func brief(v any) string {
	b, err := yaml.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return s
}

// evaluateExpectations checks every expectation against the scenario's rendered objects.
func evaluateExpectations(t *testing.T, expects []expectation, objs []renderedObject, unresolved []string) {
	t.Helper()
	for _, e := range expects {
		var matched []renderedObject
		for _, o := range objs {
			if e.selects(o) {
				matched = append(matched, o)
			}
		}
		switch {
		case e.Count != nil && len(matched) != *e.Count:
			t.Errorf("expect %s: matched %d objects, want %d%s", e, len(matched), *e.Count, candidates(e, objs, matched, unresolved))
			continue
		case e.Count == nil && len(matched) == 0:
			t.Errorf("expect %s: matched no rendered object%s", e, candidates(e, objs, nil, unresolved))
			continue
		}
		for _, o := range matched {
			for _, f := range e.Fields {
				if why := f.check(o.Object); why != "" {
					t.Errorf("expect %s: %s\n  object: %s", e, why, o)
				}
			}
		}
	}
}

// candidates names what the expectation could have meant, so a renamed object or a policy that
// rendered nothing is diagnosable from the failure alone.
func candidates(e expectation, objs, matched []renderedObject, unresolved []string) string {
	var lines []string
	if len(matched) > 0 {
		for _, o := range matched {
			lines = append(lines, "matched  "+o.String())
		}
	} else {
		for _, o := range objs {
			if o.kind() == e.Kind {
				lines = append(lines, "same kind  "+o.String())
			}
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

// --- paths ---------------------------------------------------------------------------------------
//
// A path is dot-separated keys with bracketed steps:
//
//	spec.components[kind=postgres].managed   first list element whose kind is postgres; it must be
//	                                         the only one, so a duplicated entry fails
//	spec.containers[0].image                 list index
//	stringData["clair-config.yaml"]          a key holding dots, quoted

type pathStep struct {
	key    string // map key
	index  int    // list index, when isIndex
	selKey string // list selector key, when selVal is set
	selVal string
	kind   int
}

const (
	stepKey = iota
	stepIndex
	stepSelect
)

func parsePath(p string) ([]pathStep, error) {
	var steps []pathStep
	for i := 0; i < len(p); {
		switch {
		case p[i] == '.':
			if i == 0 || i == len(p)-1 || p[i+1] == '.' || p[i+1] == '[' {
				return nil, fmt.Errorf("path %q: misplaced '.' at %d", p, i)
			}
			i++
		case p[i] == '[':
			end := strings.IndexByte(p[i:], ']')
			if strings.HasPrefix(p[i:], `["`) {
				end = strings.Index(p[i:], `"]`)
				if end < 0 {
					return nil, fmt.Errorf("path %q: unterminated [\"...\"] at %d", p, i)
				}
				steps = append(steps, pathStep{key: p[i+2 : i+end], kind: stepKey})
				i += end + 2
				continue
			}
			if end < 0 {
				return nil, fmt.Errorf("path %q: unterminated [ at %d", p, i)
			}
			inner := p[i+1 : i+end]
			if k, v, ok := strings.Cut(inner, "="); ok {
				if k == "" {
					return nil, fmt.Errorf("path %q: empty selector key at %d", p, i)
				}
				steps = append(steps, pathStep{selKey: k, selVal: v, kind: stepSelect})
			} else if n, err := strconv.Atoi(inner); err == nil && n >= 0 {
				steps = append(steps, pathStep{index: n, kind: stepIndex})
			} else {
				return nil, fmt.Errorf("path %q: [%s] is neither an index, a key=value selector nor a quoted key", p, inner)
			}
			i += end + 1
		default:
			j := i
			for j < len(p) && p[j] != '.' && p[j] != '[' {
				j++
			}
			steps = append(steps, pathStep{key: p[i:j], kind: stepKey})
			i = j
		}
	}
	if len(steps) == 0 {
		return nil, fmt.Errorf("path %q is empty", p)
	}
	return steps, nil
}

// lookupPath reports the value at steps and whether it exists. A step that meets the wrong type is
// "not found", since that is what the object says; an ambiguous selector is an error.
func lookupPath(v any, steps []pathStep) (any, bool, error) {
	cur := v
	for _, s := range steps {
		switch s.kind {
		case stepKey:
			m, ok := cur.(map[string]any)
			if !ok {
				return nil, false, nil
			}
			if cur, ok = m[s.key]; !ok {
				return nil, false, nil
			}
		case stepIndex:
			l, ok := cur.([]any)
			if !ok || s.index >= len(l) {
				return nil, false, nil
			}
			cur = l[s.index]
		case stepSelect:
			l, ok := cur.([]any)
			if !ok {
				return nil, false, nil
			}
			var hits []any
			for _, el := range l {
				if m, ok := el.(map[string]any); ok {
					if got, has := m[s.selKey]; has && fmt.Sprint(got) == s.selVal {
						hits = append(hits, el)
					}
				}
			}
			switch len(hits) {
			case 0:
				return nil, false, nil
			case 1:
				cur = hits[0]
			default:
				return nil, false, fmt.Errorf("[%s=%s] matches %d list entries; a selector must match exactly one", s.selKey, s.selVal, len(hits))
			}
		}
	}
	return cur, true, nil
}

func TestScenarioPaths(t *testing.T) {
	var obj map[string]any
	if err := yaml.Unmarshal([]byte(`
spec:
  components:
    - {kind: postgres, managed: false}
    - {kind: redis, managed: true}
    - {kind: redis, managed: false}
  list: [a, b]
stringData:
  clair-config.yaml: "indexer: {}"
`), &obj); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		path    string
		want    any
		found   bool
		wantErr bool
	}{
		{"spec.components[kind=postgres].managed", false, true, false},
		{"spec.components[kind=clair].managed", nil, false, false},
		{"spec.components[kind=redis]", nil, false, true}, // duplicated entry
		{"spec.list[1]", "b", true, false},
		{"spec.list[2]", nil, false, false},
		{`stringData["clair-config.yaml"]`, "indexer: {}", true, false},
		{"stringData.clair-config", nil, false, false},
		{"spec.list.foo", nil, false, false}, // key step on a list
	}
	for _, c := range cases {
		steps, err := parsePath(c.path)
		if err != nil {
			t.Errorf("parsePath(%q): %v", c.path, err)
			continue
		}
		got, found, err := lookupPath(obj, steps)
		if (err != nil) != c.wantErr || found != c.found || !reflect.DeepEqual(got, c.want) {
			t.Errorf("lookupPath(%q) = %v, %v, %v; want %v, %v, err=%v", c.path, got, found, err, c.want, c.found, c.wantErr)
		}
	}
	for _, bad := range []string{"", ".a", "a.", "a..b", "a[", `a["b`, "a[x]", "a[=b]", "a.[0]"} {
		if _, err := parsePath(bad); err == nil {
			t.Errorf("parsePath(%q): expected an error", bad)
		}
	}
	for _, bad := range []string{
		"path: a\nequal: 1",                // typo
		"path: a\nequals: 1\nabsent: true", // two operators
		"equals: 1",                        // no path
		"path: a\ncontains: 3",             // non-string
		"path: a\nabsent: false",
	} {
		var f fieldCheck
		if err := yaml.Unmarshal([]byte(bad), &f); err == nil {
			t.Errorf("fieldCheck %q: expected an error", bad)
		}
	}
}
