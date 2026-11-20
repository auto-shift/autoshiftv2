//go:build integration

package resolver

// Adversarial input tests for stable/project-onboarding. Each test asserts the contract in
// policies/stable/project-onboarding/README.md, and a failing test means the policy breaks it.
// Tests never edit the policy templates.

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/auto-shift/autoshiftv2/tools/internal/labels"
)

// stressRunInput is runOnboardingAll without t.Fatal on a resolution failure: hostile input is expected
// to break things, and the test must say how.
func stressRunInput(t *testing.T, testdataDir string, clusterLabels map[string]string, mutate func(cfg map[string]interface{})) ChartResult {
	t.Helper()
	if testdataDir == "" {
		testdataDir = filepath.Join(repoRoot(t), "tools", "testdata")
	}
	root := repoRoot(t)
	valuesDir := filepath.Join(root, "autoshift", "values")
	configs, err := ExtractExampleConfigs(valuesDir)
	if err != nil {
		t.Fatalf("ExtractExampleConfigs: %v", err)
	}
	raw, _ := json.Marshal(configs)
	var cloned ExampleConfigs
	if err := json.Unmarshal(raw, &cloned); err != nil {
		t.Fatalf("clone configs: %v", err)
	}
	if mutate != nil {
		mutate(cloned.HubConfig)
	}
	declared, err := labels.ExtractDeclaredFromTree(valuesDir, false)
	if err != nil {
		t.Fatalf("ExtractDeclaredFromTree: %v", err)
	}
	ctx := HubContext{ManagedClusterName: "lint-cluster", ManagedClusterLabels: BuildSyntheticLabels(declared)}
	for k, v := range clusterLabels {
		ctx.ManagedClusterLabels[k] = v
	}
	syntheticCMs, err := GenerateSyntheticConfigMaps(&cloned, ctx.ManagedClusterName, "policies-autoshift")
	if err != nil {
		t.Fatalf("GenerateSyntheticConfigMaps: %v", err)
	}
	testResources, _ := LoadTestResources(testdataDir)
	seed := append(syntheticCMs, testResources...)
	r, err := NewResolver(seed)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	spokeR, err := NewSpokeResolver(seed)
	if err != nil {
		t.Fatalf("NewSpokeResolver: %v", err)
	}
	only := func(policy string) bool { return policy == projectOnboardingPolicy }
	keep := onboardingShardsFor(&cloned, testResources, false)
	_, results, err := runPipelineTransform(filepath.Join(root, "policies"), only, cachedRender, onboardingShardFilter(t, keep), ctx, nil, r, spokeR, declared, &cloned, testdataDir)
	if err != nil {
		t.Fatalf("runPipeline: %v", err)
	}
	for _, res := range results {
		if res.Policy == projectOnboardingPolicy {
			return res
		}
	}
	t.Fatalf("%s not found in pipeline results", projectOnboardingPolicy)
	return ChartResult{}
}

// stressProjectsInput is stressRunInput with a mutation of config.projects only.
func stressProjectsInput(t *testing.T, mutate func(projects map[string]interface{})) ChartResult {
	t.Helper()
	return stressRunInput(t, "", nil, func(cfg map[string]interface{}) { mutate(cfg["projects"].(map[string]interface{})) })
}

// stressHealthy reports why res is not a cleanly resolved policy (hub error, spoke error, YAML
// error, <no value> leak, unparsable object-templates-raw), or "" when it is. A hostile value in one
// project must never produce any of these: the contract is a per-project status error.
func stressHealthy(t *testing.T, res ChartResult) string {
	t.Helper()
	var why []string
	if res.Err != nil {
		why = append(why, fmt.Sprintf("render error: %v", res.Err))
	}
	if !res.ResolveOK {
		why = append(why, fmt.Sprintf("hub resolution failed: %v", res.ResolveWarns))
	}
	if len(res.SpokeWarns) > 0 {
		why = append(why, fmt.Sprintf("spoke resolution failed: %v", res.SpokeWarns))
	}
	if len(res.YAMLErrors) > 0 {
		why = append(why, fmt.Sprintf("YAML errors: %v", res.YAMLErrors))
	}
	if strings.Contains(res.ResolvedYAML, "<no value>") {
		why = append(why, "<no value> leaked into the output")
	}
	if len(why) == 0 {
		if _, err := stressParse(res); err != nil {
			why = append(why, err.Error())
		}
	}
	return strings.Join(why, "; ")
}

// stressParse is onboardingObjects returning an error instead of failing the test.
func stressParse(res ChartResult) ([]onboardingObject, error) {
	return parseOnboardingObjects(res.ResolvedYAML)
}

// skipNonBMP skips a case whose input holds a rune outside the Basic Multilingual Plane. The
// harness resolves spoke templates over the whole Policy converted to YAML (yaml.v3 switches the
// object-templates-raw scalar to double-quoted style for such a rune, which the template parser
// cannot read), while config-policy-controller parses object-templates-raw directly
// (InputIsYAML). A failure there is a harness artifact, not a policy finding.
func skipNonBMP(t *testing.T, values ...string) {
	t.Helper()
	for _, v := range values {
		for _, r := range v {
			if r > 0xFFFF {
				t.Skipf("non-BMP rune in %q: harness cannot resolve it (see skipNonBMP)", v)
			}
		}
	}
}

// stressObjs requires a healthy result and returns its objects.
func stressObjs(t *testing.T, res ChartResult) []onboardingObject {
	t.Helper()
	if why := stressHealthy(t, res); why != "" {
		t.Fatalf("the policy did not resolve cleanly: %s", why)
	}
	objs, _ := stressParse(res)
	return objs
}

// projectObjects returns the rendered (not mustnothave, not status) objects of a namespace, plus
// the cluster-scoped objects labelled for project.
func projectObjects(objs []onboardingObject, project, namespace string) []onboardingObject {
	var out []onboardingObject
	for _, o := range objs {
		if isStatus(o) || o.complianceType == "mustnothave" {
			continue
		}
		meta, _ := o.obj["metadata"].(map[string]interface{})
		lbls := stringMap(meta, "labels")
		if meta["namespace"] == namespace || (o.obj["kind"] == "Namespace" && meta["name"] == namespace) ||
			(meta["namespace"] == nil && lbls["autoshift.io/project"] == project && o.obj["kind"] != "Namespace") {
			out = append(out, o)
		}
	}
	return out
}

func TestStress_Baseline_Healthy(t *testing.T) {
	t.Parallel()
	res := stressRunInput(t, "", nil, nil)
	if why := stressHealthy(t, res); why != "" {
		t.Fatalf("baseline: %s", why)
	}
}

func proj(projects map[string]interface{}, name string) map[string]interface{} {
	return projects[name].(map[string]interface{})
}

// injectionPayloads are strings that must stay inert data wherever a user value is rendered.
var injectionPayloads = map[string]string{
	"open delim":    "x{{y",
	"close delim":   "x}}y",
	"fail dq":       `{{ fail "boom" }}`,
	"fail backtick": "{{ fail `boom` }}",
	"hub fail":      `{{hub fail "boom" hub}}`,
	"trim comment":  "{{- /* c */ -}}",
	"lookup secret": "{{ (lookup `v1` `Secret` `openshift-config` `pull-secret`).data }}",
	"shell":         "$(id)`id`",
	"yaml doc":      "a\n---\nkind: Secret",
	"yaml anchor":   "&a *a !!binary",
	"colon hash":    "a: b # c",
}

// A group name is free-form data. Whatever it holds, the policy must still resolve and the other
// projects must still render: the group is rendered literally or the project is reported.
func TestStress_Groups_TemplateInjection(t *testing.T) {
	t.Parallel()
	for name, payload := range injectionPayloads {
		t.Run(name, func(t *testing.T) {
			res := stressProjectsInput(t, func(projects map[string]interface{}) {
				proj(projects, "payments-api")["groups"].(map[string]interface{})["read"] = payload
			})
			if why := stressHealthy(t, res); why != "" {
				t.Fatalf("group read = %q broke the whole policy: %s", payload, why)
			}
			objs, _ := stressParse(res)
			otherProjectsRender(t, objs, "payments-api")
			rb := roleBindingNamed(objs, "payments-api", "autoshift-read")
			if rb != nil {
				if got := rb["subjects"].([]interface{})[0].(map[string]interface{})["name"]; got != payload {
					t.Errorf("group read = %q rendered as %q", payload, got)
				}
			} else if onboardingStatus(t, objs)["payments-api"] == "" {
				t.Errorf("group read = %q: no RoleBinding and no status error", payload)
			}
		})
	}
}

// The same injection through a placeholder value: team fills the default group templates.
func TestStress_Placeholders_TemplateInjection(t *testing.T) {
	t.Parallel()
	for name, payload := range injectionPayloads {
		t.Run(name, func(t *testing.T) {
			res := stressProjectsInput(t, func(projects map[string]interface{}) {
				proj(projects, "reporting")["team"] = payload
			})
			if why := stressHealthy(t, res); why != "" {
				t.Fatalf("reporting team = %q broke the whole policy: %s", payload, why)
			}
			objs, _ := stressParse(res)
			otherProjectsRender(t, objs, "reporting")
			if rb := roleBindingNamed(objs, "reporting", "autoshift-admin"); rb != nil {
				if got := rb["subjects"].([]interface{})[0].(map[string]interface{})["name"]; !strings.Contains(fmt.Sprint(got), payload) {
					t.Errorf("reporting team = %q: admin group rendered as %q, the value was executed as a template", payload, got)
				}
			}
		})
	}
}

// projectDefaults.roles values are rendered as the roleRef name. A string value must never break
// the policy for every project.
func TestStress_Roles_TemplateInjection(t *testing.T) {
	t.Parallel()
	for name, payload := range injectionPayloads {
		t.Run(name, func(t *testing.T) {
			res := stressRunInput(t, "", nil, func(cfg map[string]interface{}) {
				cfg["projectDefaults"].(map[string]interface{})["roles"].(map[string]interface{})["read"] = payload
			})
			if why := stressHealthy(t, res); why != "" {
				t.Fatalf("roles.read = %q broke the whole policy: %s", payload, why)
			}
		})
	}
}

// Every other user string that reaches the output: annotations (catalog and displayName,
// description, requester) and label templates. Each must render literally or be reported.
func TestStress_Annotations_TemplateInjection(t *testing.T) {
	t.Parallel()
	for name, payload := range injectionPayloads {
		t.Run(name, func(t *testing.T) {
			res := stressRunInput(t, "", nil, func(cfg map[string]interface{}) {
				cfg["projectDefaults"].(map[string]interface{})["annotations"] = map[string]interface{}{
					"owner": map[string]interface{}{"key": "example.com/owner", "value": "{owner}"},
				}
				p := proj(cfg["projects"].(map[string]interface{}), "reporting")
				p["owner"] = payload
				p["displayName"] = payload
				p["description"] = payload
				p["requester"] = payload
			})
			objs := stressObjs(t, res)
			otherProjectsRender(t, objs, "")
			ann := stringMap(namespaceNamed(objs, "reporting"), "annotations")
			for _, k := range []string{"example.com/owner", "openshift.io/display-name", "openshift.io/description", "openshift.io/requester"} {
				if ann[k] != payload {
					t.Errorf("annotation %s = %q, want %q literally", k, ann[k], payload)
				}
			}
		})
	}
}

// A placeholder whose name has a character outside [A-Za-z0-9_] ({cost-center}, {team.name}) is
// still a placeholder by the template grammar ({field}). Unresolved, it must skip the label,
// annotation or group, never render the braces literally.
func TestStress_Placeholders_UnresolvedNonWordName(t *testing.T) {
	t.Parallel()
	for _, ph := range []string{"{business-unit}", "{team.name}", "{team name}", "{Team-ID}"} {
		t.Run(ph, func(t *testing.T) {
			res := stressRunInput(t, "", nil, func(cfg map[string]interface{}) {
				d := cfg["projectDefaults"].(map[string]interface{})
				d["annotations"] = map[string]interface{}{"bu": map[string]interface{}{"key": "example.com/bu", "value": "bu-" + ph}}
				d["groups"].(map[string]interface{})["write"] = "K8S_" + ph + "_DEVELOPERS"
			})
			objs := stressObjs(t, res)
			for _, project := range []string{"payments-api", "reporting", "legacy-app"} {
				if v, ok := stringMap(namespaceNamed(objs, project), "annotations")["example.com/bu"]; ok {
					t.Errorf("%s: annotation example.com/bu = %q rendered with an unresolved placeholder", project, v)
				}
				if rb := roleBindingNamed(objs, project, "autoshift-write"); rb != nil {
					t.Errorf("%s: autoshift-write bound to %v with an unresolved placeholder", project, rb["subjects"])
				}
			}
		})
	}
}

// Same, for the admin group: unresolved means no resolvable admin group, so the project is skipped
// and reported, never bound to a literal group name.
func TestStress_Placeholders_UnresolvedAdminGroup(t *testing.T) {
	t.Parallel()
	res := stressRunInput(t, "", nil, func(cfg map[string]interface{}) {
		proj(cfg["projects"].(map[string]interface{}), "reporting")["groups"] = map[string]interface{}{"admin": "K8S_{business-unit}_ADMINS"}
	})
	objs := stressObjs(t, res)
	if rb := roleBindingNamed(objs, "reporting", "autoshift-admin"); rb != nil {
		t.Errorf("reporting admin bound to %v: an unresolved placeholder must not bind", rb["subjects"])
	}
	if got := onboardingStatus(t, objs)["reporting"]; got != "no resolvable admin group" {
		t.Errorf("status[reporting] = %q, want no resolvable admin group", got)
	}
}

// A placeholder value is data, not a template: team: '{zone}' must not expand {zone}. Today the
// result depends on field name order (fields are replaced alphabetically, so a value naming a
// later field is expanded, one naming an earlier field is not).
func TestStress_Placeholders_ValueReexpanded(t *testing.T) {
	t.Parallel()
	res := stressProjectsInput(t, func(projects map[string]interface{}) {
		p := proj(projects, "reporting")
		p["team"] = "{zone}"
		p["zone"] = "eu1"
	})
	objs := stressObjs(t, res)
	if v := stringMap(namespaceNamed(objs, "reporting"), "labels")["example.com/team"]; v == "eu1" {
		t.Errorf("team: '{zone}' was expanded to %q: placeholder values are re-read as templates", v)
	}
}

// A whitespace-only group name resolves to no real group. It must not bind.
func TestStress_Groups_WhitespaceName(t *testing.T) {
	t.Parallel()
	res := stressProjectsInput(t, func(projects map[string]interface{}) {
		proj(projects, "payments-api")["groups"].(map[string]interface{})["read"] = "   "
		proj(projects, "reporting")["team"] = " "
	})
	objs := stressObjs(t, res)
	if rb := roleBindingNamed(objs, "payments-api", "autoshift-read"); rb != nil {
		t.Errorf("group read '   ' bound to %v", rb["subjects"])
	}
	if rb := roleBindingNamed(objs, "reporting", "autoshift-admin"); rb != nil {
		t.Errorf("team ' ' bound reporting admin to %v", rb["subjects"])
	}
}

// groups and roles take only admin, write and read. Any other level is silently ignored today.
func TestStress_Groups_UnknownLevelIgnored(t *testing.T) {
	t.Parallel()
	res := stressProjectsInput(t, func(projects map[string]interface{}) {
		proj(projects, "payments-api")["groups"].(map[string]interface{})["owner"] = "K8S_OWNERS"
	})
	objs := stressObjs(t, res)
	if onboardingStatus(t, objs)["payments-api"] == "" {
		t.Error("groups.owner is not admin, write or read: want it reported, it is silently dropped")
	}
}

// ---- Quotas -------------------------------------------------------------------------------------

// A numeric 0 is a real quota (count/services.loadbalancers: 0 forbids load balancers). "Numbers are
// accepted and rendered as strings", and only ” and null are documented as ignored overrides.
func TestStress_Quota_ZeroOverrideIgnored(t *testing.T) {
	t.Parallel()
	res := stressRunInput(t, "", nil, func(cfg map[string]interface{}) {
		small := cfg["projectDefaults"].(map[string]interface{})["sizes"].(map[string]interface{})["small"].(map[string]interface{})
		small["quota"].(map[string]interface{})["services.loadbalancers"] = "2"
		proj(cfg["projects"].(map[string]interface{}), "payments-api")["quota"] = map[string]interface{}{"pods": 0, "services.loadbalancers": 0}
	})
	hard := quotaHard(stressObjs(t, res), "payments-api")
	if hard["pods"] != "0" || hard["services.loadbalancers"] != "0" {
		t.Errorf("quota override pods: 0, services.loadbalancers: 0 rendered %v; want both \"0\" (the size's value is more open than declared)", hard)
	}
}

// Same for a string "0" (control: documented to work).
func TestStress_Quota_StringZeroOverride(t *testing.T) {
	t.Parallel()
	res := stressProjectsInput(t, func(projects map[string]interface{}) {
		proj(projects, "payments-api")["quota"] = map[string]interface{}{"pods": "0"}
	})
	if hard := quotaHard(stressObjs(t, res), "payments-api"); hard["pods"] != "0" {
		t.Errorf("quota pods '0' rendered %v", hard["pods"])
	}
}

// A ResourceQuota key without a "/" must be a standard quota resource name, or the API server
// rejects the whole object. The contract reports "a key that is not a valid resource name" and skips
// only the ResourceQuota; today these render a ResourceQuota the API server refuses.
func TestStress_Quota_NonQuotaResourceName(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"request.cpu", "cpus", "foo", "limits.gpu"} {
		t.Run(key, func(t *testing.T) {
			res := stressProjectsInput(t, func(projects map[string]interface{}) {
				proj(projects, "payments-api")["quota"] = map[string]interface{}{key: "1"}
			})
			objs := stressObjs(t, res)
			if hard := quotaHard(objs, "payments-api"); hard != nil {
				if _, ok := hard[key]; ok {
					t.Errorf("quota key %q is not a ResourceQuota resource name, but rendered in %v (the API server rejects the object)", key, hard)
				}
			}
		})
	}
}

// Same for LimitRange (type Container): an unqualified key must be cpu, memory, ephemeral-storage
// or hugepages-*.
func TestStress_LimitRange_NonContainerResourceName(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"cpus", "pods", "storage"} {
		t.Run(key, func(t *testing.T) {
			res := stressProjectsInput(t, func(projects map[string]interface{}) {
				proj(projects, "payments-api")["limitRange"] = map[string]interface{}{"max": map[string]interface{}{key: "1"}}
			})
			objs := stressObjs(t, res)
			if item := containerLimits(t, objs, "payments-api"); item != nil {
				if m, _ := item["max"].(map[string]interface{}); m != nil {
					if _, ok := m[key]; ok {
						t.Errorf("limitRange.max key %q is not a Container resource, but rendered (the API server rejects the object)", key)
					}
				}
			}
		})
	}
}

// Quantity boundaries: what must be rejected (and only the object skipped) and what must render.
func TestStress_Quota_QuantityBoundaries(t *testing.T) {
	t.Parallel()
	bad := []interface{}{"1Gi ", " 1Gi", "1 Gi", "-1", "1gi", "1GB", "0x10", "1e", "1.2.3", "∞", "١", "   ", "1Gi\n", "NaN", "Infinity"}
	for _, v := range bad {
		t.Run(fmt.Sprintf("bad %q", v), func(t *testing.T) {
			res := stressProjectsInput(t, func(projects map[string]interface{}) {
				proj(projects, "payments-api")["quota"] = map[string]interface{}{"requests.cpu": v}
			})
			objs := stressObjs(t, res)
			hard := quotaHard(objs, "payments-api")
			if v == "" || v == "   " {
				// '' is an ignored override (documented), so the size's value stays. Whitespace is not empty.
				if v == "" {
					if hard["requests.cpu"] != "4" {
						t.Errorf("'' override: want the size value 4, got %v", hard)
					}
					return
				}
			}
			if hard != nil {
				t.Errorf("requests.cpu %q is not a quantity, want no ResourceQuota, got %v", v, hard)
			}
			if onboardingStatus(t, objs)["payments-api"] == "" {
				t.Errorf("requests.cpu %q: want a status error", v)
			}
			otherProjectsRender(t, objs, "")
		})
	}
	good := map[string]interface{}{"0.5": "0.5", "500m": "500m", "1e3": "1e3", "+1": "+1", ".5": ".5", "5.": "5.", "1Ei": "1Ei"}
	for in, want := range good {
		t.Run("good "+in, func(t *testing.T) {
			res := stressProjectsInput(t, func(projects map[string]interface{}) {
				proj(projects, "payments-api")["quota"] = map[string]interface{}{"requests.cpu": in}
			})
			if hard := quotaHard(stressObjs(t, res), "payments-api"); hard["requests.cpu"] != want {
				t.Errorf("requests.cpu %q rendered %v", in, hard["requests.cpu"])
			}
		})
	}
}

// "An override with an empty value, such as pods: ” or pods: null, is ignored by the merge and the
// size's value stays." Today both are reported as invalid quantities and the whole ResourceQuota is
// dropped (and the LimitRange likewise for limitRange overrides).
func TestStress_Quota_EmptyOverrideRejected(t *testing.T) {
	t.Parallel()
	for _, v := range []interface{}{"", nil} {
		t.Run(fmt.Sprintf("quota %#v", v), func(t *testing.T) {
			res := stressProjectsInput(t, func(projects map[string]interface{}) {
				proj(projects, "payments-api")["quota"] = map[string]interface{}{"requests.cpu": v}
			})
			objs := stressObjs(t, res)
			if hard := quotaHard(objs, "payments-api"); hard["requests.cpu"] != "4" {
				t.Errorf("quota requests.cpu: %#v must be ignored (size value 4 stays); got ResourceQuota %v, status %q", v, hard, onboardingStatus(t, objs)["payments-api"])
			}
		})
		t.Run(fmt.Sprintf("limitRange %#v", v), func(t *testing.T) {
			res := stressProjectsInput(t, func(projects map[string]interface{}) {
				proj(projects, "payments-api")["limitRange"] = map[string]interface{}{"default": map[string]interface{}{"cpu": v}}
			})
			objs := stressObjs(t, res)
			item := containerLimits(t, objs, "payments-api")
			def, _ := item["default"].(map[string]interface{})
			if def["cpu"] != "500m" {
				t.Errorf("limitRange.default.cpu: %#v must be ignored (size value 500m stays); got %v, status %q", v, item, onboardingStatus(t, objs)["payments-api"])
			}
		})
	}
}

// "Numbers are accepted and rendered as strings." A large integer is rendered in Go float notation
// (1e+06), which the API server canonicalises to 1M, so the exact ResourceQuota never matches.
func TestStress_Quota_LargeIntegerExponent(t *testing.T) {
	t.Parallel()
	res := stressProjectsInput(t, func(projects map[string]interface{}) {
		proj(projects, "payments-api")["quota"] = map[string]interface{}{"pods": 1000000}
	})
	if hard := quotaHard(stressObjs(t, res), "payments-api"); hard["pods"] != "1000000" {
		t.Errorf("quota pods: 1000000 (number) rendered %q, want \"1000000\"", hard["pods"])
	}
}

// A limitRange field that is not a map is reported (limitRange.max is not a map). A falsy non-map
// (0, false) is silently dropped instead.
func TestStress_LimitRange_FalsyFieldDropped(t *testing.T) {
	t.Parallel()
	for _, v := range []interface{}{0, false} {
		t.Run(fmt.Sprint(v), func(t *testing.T) {
			res := stressProjectsInput(t, func(projects map[string]interface{}) {
				proj(projects, "payments-api")["limitRange"] = map[string]interface{}{"max": v}
			})
			if got := onboardingStatus(t, stressObjs(t, res))["payments-api"]; got != "limitRange.max is not a map" {
				t.Errorf("limitRange.max: %v, want status 'limitRange.max is not a map', got %q", v, got)
			}
		})
	}
}

// size given as a non-string scalar: the contract skips a size "that is not a string".
func TestStress_Size_NonStringScalar(t *testing.T) {
	t.Parallel()
	for _, v := range []interface{}{true, 1, 0.5} {
		t.Run(fmt.Sprint(v), func(t *testing.T) {
			res := stressProjectsInput(t, func(projects map[string]interface{}) { proj(projects, "payments-api")["size"] = v })
			objs := stressObjs(t, res)
			got := onboardingStatus(t, objs)["payments-api"]
			if got != "size is not a string" {
				t.Errorf("size: %v, want status 'size is not a string', got %q", v, got)
			}
			if namespaceNamed(objs, "payments-api") != nil {
				t.Errorf("size: %v must skip the project", v)
			}
		})
	}
}

// size: 0 or false is not a string either; today it is treated as "no size", silently.
func TestStress_Size_FalsyScalarSilent(t *testing.T) {
	t.Parallel()
	for _, v := range []interface{}{0, false} {
		t.Run(fmt.Sprint(v), func(t *testing.T) {
			res := stressProjectsInput(t, func(projects map[string]interface{}) { proj(projects, "payments-api")["size"] = v })
			if got := onboardingStatus(t, stressObjs(t, res))["payments-api"]; !strings.Contains(got, "size") {
				t.Errorf("size: %v, want a size error, got %q (the project silently lost its quota)", v, got)
			}
		})
	}
}

// ---- Labels and annotations ---------------------------------------------------------------------

func setCatalog(kind string, entries map[string]interface{}) func(cfg map[string]interface{}) {
	return func(cfg map[string]interface{}) {
		d := cfg["projectDefaults"].(map[string]interface{})
		c, _ := d[kind].(map[string]interface{})
		if c == nil {
			c = map[string]interface{}{}
			d[kind] = c
		}
		for k, v := range entries {
			c[k] = v
		}
	}
}

func kv(key, value string) map[string]interface{} {
	return map[string]interface{}{"key": key, "value": value}
}

// A label key or value that is not valid Kubernetes label syntax is skipped and reported; the rest
// of the project renders.
func TestStress_Labels_InvalidSyntax(t *testing.T) {
	t.Parallel()
	long := func(n int) string { return strings.Repeat("a", n) }
	cases := map[string][2]string{
		"value 64":            {"example.com/x", long(64)},
		"value newline":       {"example.com/x", "a\nb"},
		"value space":         {"example.com/x", "a b"},
		"value leading dash":  {"example.com/x", "-a"},
		"value trailing dot":  {"example.com/x", "a."},
		"value unicode":       {"example.com/x", "café"},
		"value emoji":         {"example.com/x", "a🙂"},
		"value braces":        {"example.com/x", "{{x}}"},
		"key two slashes":     {"a/b/c", "x"},
		"key name 64":         {"example.com/" + long(64), "x"},
		"key prefix 254":      {long(254) + "/x", "x"},
		"key prefix upper":    {"Example.com/x", "x"},
		"key empty prefix":    {"/x", "x"},
		"key empty name":      {"example.com/", "x"},
		"key unicode":         {"exämple.com/x", "x"},
		"key leading dot":     {".example.com/x", "x"},
		"key prefix dash end": {"example-.com/x", "x"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			skipNonBMP(t, c[0], c[1])
			res := stressRunInput(t, "", nil, func(cfg map[string]interface{}) {
				proj(cfg["projects"].(map[string]interface{}), "reporting")["labels"] = map[string]interface{}{"bad": kv(c[0], c[1])}
			})
			objs := stressObjs(t, res)
			lbls := stringMap(namespaceNamed(objs, "reporting"), "labels")
			if _, ok := lbls[c[0]]; ok {
				t.Errorf("label %q=%q is invalid but rendered", c[0], c[1])
			}
			// {{x}} holds the placeholder {x}, unresolved: skipped without a report is the contract.
			if c[1] != "{{x}}" && !strings.Contains(onboardingStatus(t, objs)["reporting"], "is not a valid Kubernetes label") {
				t.Errorf("label %q=%q: want reported", c[0], c[1])
			}
			if lbls["example.com/team"] != "reporting" {
				t.Errorf("rest of reporting must render, labels %v", lbls)
			}
			otherProjectsRender(t, objs, "")
		})
	}
	// Boundaries that are valid.
	okCases := map[string][2]string{
		"value 63":       {"example.com/x", long(63)},
		"value empty":    {"example.com/y", ""},
		"name 63":        {"example.com/" + long(63), "x"},
		"prefix 253":     {strings.Repeat("a.", 126) + "a/x", "x"},
		"no prefix":      {"plain", "x"},
		"leading digit":  {"0example.com/1x", "1"},
		"underscore dot": {"example.com/a_b.c", "a_b.c"},
	}
	for name, c := range okCases {
		t.Run("ok "+name, func(t *testing.T) {
			res := stressRunInput(t, "", nil, func(cfg map[string]interface{}) {
				proj(cfg["projects"].(map[string]interface{}), "reporting")["labels"] = map[string]interface{}{"ok": kv(c[0], c[1])}
			})
			objs := stressObjs(t, res)
			if c[1] == "" {
				// An empty value is a resolved-empty template: skipped by the skip rules.
				return
			}
			if got, ok := stringMap(namespaceNamed(objs, "reporting"), "labels")[c[0]]; !ok || got != c[1] {
				t.Errorf("valid label %q=%q not rendered (status %v)", c[0], c[1], onboardingStatus(t, objs))
			}
		})
	}
}

// An annotation key must be a valid qualified name, or the API server rejects the whole Namespace
// (and every later update of it). A catalog annotation with an invalid key is rendered as is.
func TestStress_Annotations_InvalidKey(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"bad key", "a/b/c", "-x", "example.com/", strings.Repeat("a", 254) + "/x", "x{{y", "Example.com/x"} {
		t.Run(key, func(t *testing.T) {
			res := stressRunInput(t, "", nil, func(cfg map[string]interface{}) {
				proj(cfg["projects"].(map[string]interface{}), "reporting")["annotations"] = map[string]interface{}{"bad": kv(key, "v")}
			})
			objs := stressObjs(t, res)
			if _, ok := stringMap(namespaceNamed(objs, "reporting"), "annotations")[key]; ok {
				t.Errorf("annotation key %q is not a qualified name but is rendered on the Namespace (the API server rejects it)", key)
			}
		})
	}
}

// Annotation values are free text: newlines, quotes, 10k characters render literally.
func TestStress_Annotations_FreeTextValues(t *testing.T) {
	t.Parallel()
	for name, v := range map[string]string{
		"newline": "line1\nline2", "quotes": `a"b'c`, "doc": "---\nkind: Secret", "10k": strings.Repeat("x", 10000),
		"unicode": "café 🙂", "tab": "a\tb", "backslash": `a\nb\\`,
	} {
		t.Run(name, func(t *testing.T) {
			skipNonBMP(t, v)
			res := stressProjectsInput(t, func(projects map[string]interface{}) { proj(projects, "reporting")["description"] = v })
			if got := stringMap(namespaceNamed(stressObjs(t, res), "reporting"), "annotations")["openshift.io/description"]; got != v {
				t.Errorf("description %q rendered %q", v, got)
			}
		})
	}
}

// displayName is a string annotation. A map or list is not a display name: rendered today as Go's
// fmt form (map[a:b]), silently.
func TestStress_DisplayName_NonString(t *testing.T) {
	t.Parallel()
	for name, v := range map[string]interface{}{"map": map[string]interface{}{"a": "b"}, "list": []interface{}{"a"}} {
		t.Run(name, func(t *testing.T) {
			res := stressProjectsInput(t, func(projects map[string]interface{}) { proj(projects, "reporting")["displayName"] = v })
			objs := stressObjs(t, res)
			got, ok := stringMap(namespaceNamed(objs, "reporting"), "annotations")["openshift.io/display-name"]
			if ok && onboardingStatus(t, objs)["reporting"] == "" {
				t.Errorf("displayName %s rendered as %q with no status error", name, got)
			}
		})
	}
}

// The ownership keys are reserved; every spelling that reaches them must be refused, including
// through a placeholder and through annotations.
func TestStress_Labels_OwnershipCollision(t *testing.T) {
	t.Parallel()
	cases := map[string]map[string]interface{}{
		"project key":         {"own": kv("autoshift.io/project", "other")},
		"managed-by key":      {"own": kv("autoshift.io/managed-by", "someone")},
		"via placeholder key": {"own": kv("autoshift.io/{ownKey}", "other")},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			res := stressProjectsInput(t, func(projects map[string]interface{}) {
				p := proj(projects, "reporting")
				p["labels"] = entries
				p["ownKey"] = "project"
			})
			objs := stressObjs(t, res)
			lbls := stringMap(namespaceNamed(objs, "reporting"), "labels")
			if lbls["autoshift.io/project"] != "reporting" || lbls["autoshift.io/managed-by"] != "project-onboarding" {
				t.Errorf("ownership labels overridden: %v", lbls)
			}
			if !strings.Contains(onboardingStatus(t, objs)["reporting"], "reserved for project ownership") {
				t.Errorf("want the reserved key reported, status %v", onboardingStatus(t, objs))
			}
		})
	}
	t.Run("project-labels annotation", func(t *testing.T) {
		res := stressProjectsInput(t, func(projects map[string]interface{}) {
			proj(projects, "reporting")["annotations"] = map[string]interface{}{"x": kv("autoshift.io/project-labels", "kubernetes.io/metadata.name")}
		})
		ann := stringMap(namespaceNamed(stressObjs(t, res), "reporting"), "annotations")
		if ann["autoshift.io/project-labels"] != "example.com/environment,example.com/team" {
			t.Errorf("a catalog annotation overrode autoshift.io/project-labels: %q (with prune on it names keys to strip)", ann["autoshift.io/project-labels"])
		}
	})
}

// Platform-owned label keys a project can set on its own Namespace. network.openshift.io/policy-group
// makes the project a "router" or "monitoring" peer for every other project's NetworkPolicies and the
// admin AdminNetworkPolicies; pod-security and run-level labels lift platform guards;
// kubernetes.io/metadata.name is overwritten by the API server, so the exact Namespace never complies.
// The contract reserves only the two ownership keys.
func TestStress_Labels_PlatformKeyImpersonation(t *testing.T) {
	t.Parallel()
	for _, c := range [][2]string{
		{"network.openshift.io/policy-group", "ingress"},
		{"network.openshift.io/policy-group", "monitoring"},
		{"pod-security.kubernetes.io/enforce", "privileged"},
		{"openshift.io/run-level", "0"},
		{"kubernetes.io/metadata.name", "openshift-dns"},
	} {
		t.Run(c[0]+"="+c[1], func(t *testing.T) {
			res := stressProjectsInput(t, func(projects map[string]interface{}) {
				proj(projects, "reporting")["labels"] = map[string]interface{}{"x": kv(c[0], c[1])}
			})
			if got, ok := stringMap(namespaceNamed(stressObjs(t, res), "reporting"), "labels")[c[0]]; ok && got == c[1] {
				t.Errorf("project label %s=%s is applied to the project Namespace", c[0], c[1])
			}
		})
	}
}

// Two catalog entries that resolve to the same label key: one value silently wins.
func TestStress_Labels_DuplicateKeySilent(t *testing.T) {
	t.Parallel()
	res := stressProjectsInput(t, func(projects map[string]interface{}) {
		proj(projects, "reporting")["labels"] = map[string]interface{}{"a": kv("example.com/dup", "one"), "b": kv("example.com/dup", "two")}
	})
	if got := onboardingStatus(t, stressObjs(t, res))["reporting"]; got == "" {
		t.Error("two entries set example.com/dup to different values; want reported, one is silently dropped")
	}
}

// enabled on a catalog entry: anything but false keeps it on, so enabled: 0, "False", "no", "off"
// leave the entry enabled without a report. Rules report a non-bool enabled.
func TestStress_Labels_EnabledNonBool(t *testing.T) {
	t.Parallel()
	for _, v := range []interface{}{0, "False", "no", "off", "0"} {
		t.Run(fmt.Sprint(v), func(t *testing.T) {
			res := stressProjectsInput(t, func(projects map[string]interface{}) {
				proj(projects, "payments-api")["labels"] = map[string]interface{}{"cost-center": map[string]interface{}{"enabled": v}}
			})
			objs := stressObjs(t, res)
			_, on := stringMap(namespaceNamed(objs, "payments-api"), "labels")["example.com/cost-center"]
			if on && onboardingStatus(t, objs)["payments-api"] == "" {
				t.Errorf("enabled: %#v kept the label on with no report", v)
			}
		})
	}
}

// ---- Names, namespace, mode, adopt --------------------------------------------------------------

func TestStress_Names_DNS1123Boundaries(t *testing.T) {
	t.Parallel()
	bad := []string{strings.Repeat("a", 64), "-a", "a-", "A", "a.b", "a_b", "café", "a🙂", "", " a", "a\n", "a/b", "..", "a{{b"}
	for _, n := range bad {
		t.Run(fmt.Sprintf("project %q", n), func(t *testing.T) {
			skipNonBMP(t, n)
			res := stressProjectsInput(t, func(projects map[string]interface{}) { addProject(projects, n, "onb-fresh") })
			objs := stressObjs(t, res)
			if namespaceNamed(objs, "onb-fresh") != nil {
				t.Errorf("project %q is invalid but rendered", n)
			}
			otherProjectsRender(t, objs, "")
			if len(onboardingStatus(t, objs)) != 1 {
				t.Errorf("project %q: want one status key, got %v", n, onboardingStatus(t, objs))
			}
		})
		t.Run(fmt.Sprintf("namespace %q", n), func(t *testing.T) {
			skipNonBMP(t, n)
			res := stressProjectsInput(t, func(projects map[string]interface{}) { proj(projects, "reporting")["namespace"] = n })
			objs := stressObjs(t, res)
			if !strings.Contains(onboardingStatus(t, objs)["reporting"], "is not a valid DNS-1123 label") {
				t.Errorf("namespace %q: want reported, got %v", n, onboardingStatus(t, objs))
			}
			otherProjectsRender(t, objs, "reporting")
		})
	}
	for _, n := range []string{strings.Repeat("a", 63), "0a", "1", "a-b-c", "a--b"} {
		t.Run(fmt.Sprintf("ok %q", n), func(t *testing.T) {
			res := stressProjectsInput(t, func(projects map[string]interface{}) {
				addProject(projects, n, "")
				delete(proj(projects, n), "namespace")
			})
			objs := stressObjs(t, res)
			if namespaceNamed(objs, n) == nil {
				t.Errorf("project %q is a valid DNS-1123 label but not rendered: %v", n, onboardingStatus(t, objs))
			}
		})
	}
}

// A very long invalid project name is reported under a ConfigMap-key-safe status key. A key over 253
// characters makes the API server reject the whole status ConfigMap, losing every project's errors.
// 1000, not more: the harness writes rendered-config as JSON, and YAML caps flow keys at 1024.
func TestStress_Names_LongInvalidStatusKey(t *testing.T) {
	t.Parallel()
	n := strings.Repeat("A", 1000)
	res := stressProjectsInput(t, func(projects map[string]interface{}) { addProject(projects, n, "onb-fresh") })
	for k := range onboardingStatus(t, stressObjs(t, res)) {
		if len(k) > 253 {
			t.Errorf("status key of %d characters, a ConfigMap key is at most 253", len(k))
		}
	}
}

// Two invalid project names that sanitise to the same status key: each project's error must
// survive. Today the second overwrites the first.
func TestStress_Status_SanitisedKeyCollision(t *testing.T) {
	t.Parallel()
	res := stressProjectsInput(t, func(projects map[string]interface{}) {
		addProject(projects, "bad name", "onb-a")
		addProject(projects, "bad_name", "onb-b")
	})
	got := onboardingStatus(t, stressObjs(t, res))["bad_name"]
	if !strings.Contains(got, "bad name") || !strings.Contains(got, "project name bad_name") {
		t.Errorf("status[bad_name] = %q, want the errors of both 'bad name' and 'bad_name'", got)
	}
}

// namespace: null in a values file is how Helm deletes a key; the contract defaults namespace to the
// project key.
func TestStress_Namespace_Null(t *testing.T) {
	t.Parallel()
	for _, v := range []interface{}{nil, ""} {
		t.Run(fmt.Sprintf("%#v", v), func(t *testing.T) {
			res := stressProjectsInput(t, func(projects map[string]interface{}) { proj(projects, "reporting")["namespace"] = v })
			objs := stressObjs(t, res)
			if namespaceNamed(objs, "reporting") == nil {
				t.Errorf("namespace: %#v, want the default (the project key), status %v", v, onboardingStatus(t, objs))
			}
		})
	}
}

// Platform namespaces. With adopt: true a project takes over any existing namespace that no project
// owns, including kube-system, openshift-* and the add-on namespace that holds the status ConfigMap,
// and then applies exact metadata, quota and default-deny NetworkPolicies there. No guard exists.
func TestStress_Namespace_PlatformAdopt(t *testing.T) {
	t.Parallel()
	for _, ns := range []string{"kube-system", "default", "openshift-config", "openshift-dns", "open-cluster-management-agent-addon"} {
		t.Run(ns, func(t *testing.T) {
			res := stressProjectsInput(t, func(projects map[string]interface{}) {
				addProject(projects, "grab", ns)
				p := proj(projects, "grab")
				p["adopt"] = true
				p["mode"] = "enforce"
				p["networkProfile"] = "strict"
			})
			objs := stressObjs(t, res)
			if namespaceNamed(objs, ns) != nil {
				t.Errorf("project grab took over platform namespace %s (Namespace, NetworkPolicies %v)", ns, networkPoliciesIn(objs, ns))
			}
		})
	}
}

func TestStress_Mode_WrongTypes(t *testing.T) {
	t.Parallel()
	for _, v := range []interface{}{true, 1, []interface{}{"enforce"}, map[string]interface{}{"a": "b"}, "ENFORCE", " enforce", "enforce\n"} {
		t.Run(fmt.Sprint(v), func(t *testing.T) {
			res := stressProjectsInput(t, func(projects map[string]interface{}) { proj(projects, "reporting")["mode"] = v })
			objs := stressObjs(t, res)
			if got := remediationOf(objs, "reporting"); got != "inform" {
				t.Errorf("mode %#v rendered with %q, want inform", v, got)
			}
			if !strings.Contains(onboardingStatus(t, objs)["reporting"], "is not enforce or inform") {
				t.Errorf("mode %#v: want reported, got %v", v, onboardingStatus(t, objs))
			}
		})
	}
}

// adopt takes true. A misspelt "yes" means no adopt, silently; "True" likewise.
func TestStress_Adopt_NonBool(t *testing.T) {
	t.Parallel()
	for _, v := range []interface{}{"yes", "True", 1} {
		t.Run(fmt.Sprint(v), func(t *testing.T) {
			res := stressProjectsInput(t, func(projects map[string]interface{}) { proj(projects, "legacy-app")["adopt"] = v })
			if got := onboardingStatus(t, stressObjs(t, res))["legacy-app"]; !strings.Contains(got, "adopt") || strings.Contains(got, "set adopt: true") {
				t.Errorf("adopt: %#v is not true or false; want it reported as such, got %q", v, got)
			}
		})
	}
}

// ---- Placeholders: reserved names, non-string values, placeholder-built keys -----------------

// "{name} is always the project name." A project field called name must not replace it.
func TestStress_Placeholders_NameOverride(t *testing.T) {
	t.Parallel()
	res := stressRunInput(t, "", nil, func(cfg map[string]interface{}) {
		cfg["projectDefaults"].(map[string]interface{})["labels"].(map[string]interface{})["proj"] = kv("example.com/project", "{name}")
		proj(cfg["projects"].(map[string]interface{}), "reporting")["name"] = "payments-api"
	})
	if got := stringMap(namespaceNamed(stressObjs(t, res), "reporting"), "labels")["example.com/project"]; got != "reporting" {
		t.Errorf("reporting sets name: payments-api, {name} rendered %q, want reporting", got)
	}
}

// Placeholder values are free-form, but a map or list is not a value, and a number must render as
// written. Today a number is rendered through Go's %v (1000000 becomes 1e+06) and a map as map[a:b],
// so the RoleBinding binds a group that does not exist, silently.
func TestStress_Placeholders_NonStringValues(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		v    interface{}
		want string // "" = no admin RoleBinding, project reported
	}{
		"int":   {1000000, "K8S_1000000_ADMINS_PROD"},
		"int2":  {4711, "K8S_4711_ADMINS_PROD"},
		"float": {4.5, "K8S_4.5_ADMINS_PROD"},
		"map":   {map[string]interface{}{"a": "b"}, ""},
		"list":  {[]interface{}{"a"}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			objs := stressObjs(t, stressProjectsInput(t, func(projects map[string]interface{}) { proj(projects, "reporting")["team"] = c.v }))
			rb := roleBindingNamed(objs, "reporting", "autoshift-admin")
			var got interface{}
			if rb != nil {
				got = rb["subjects"].([]interface{})[0].(map[string]interface{})["name"]
			}
			if c.want == "" {
				if rb != nil {
					t.Errorf("team: %#v bound reporting admin to %v", c.v, got)
				}
				return
			}
			if got != c.want {
				t.Errorf("team: %#v bound reporting admin to %v, want %s", c.v, got, c.want)
			}
		})
	}
}

// A placeholder value that makes a templated label key invalid skips that label and reports it.
func TestStress_Labels_PlaceholderBuiltKey(t *testing.T) {
	t.Parallel()
	for _, v := range []string{"a/b", "x{{y", "A B", strings.Repeat("a", 64)} {
		t.Run(v, func(t *testing.T) {
			objs := stressObjs(t, stressProjectsInput(t, func(projects map[string]interface{}) { proj(projects, "reporting")["vlan"] = v }))
			lbls := stringMap(namespaceNamed(objs, "reporting"), "labels")
			if _, ok := lbls["network.example.com/vlan-"+v]; ok {
				t.Errorf("vlan %q built an invalid label key that rendered", v)
			}
			if !strings.Contains(onboardingStatus(t, objs)["reporting"], "is not a valid Kubernetes label") {
				t.Errorf("vlan %q: want the label reported, status %q", v, onboardingStatus(t, objs)["reporting"])
			}
		})
	}
}

// Platform annotations a catalog can set on the project Namespace. openshift.io/sa.scc.uid-range
// decides the UIDs restricted SCCs allow (0/10000 admits root), openshift.io/node-selector and
// scheduler.alpha.kubernetes.io/defaultTolerations move workloads onto control-plane nodes. The
// contract reserves none of them.
func TestStress_Annotations_PlatformKeys(t *testing.T) {
	t.Parallel()
	for _, c := range [][2]string{
		{"openshift.io/sa.scc.uid-range", "0/10000"},
		{"openshift.io/sa.scc.supplemental-groups", "0/10000"},
		{"openshift.io/node-selector", "node-role.kubernetes.io/master="},
		{"scheduler.alpha.kubernetes.io/defaultTolerations", `[{"operator":"Exists"}]`},
	} {
		t.Run(c[0], func(t *testing.T) {
			res := stressProjectsInput(t, func(projects map[string]interface{}) {
				proj(projects, "reporting")["annotations"] = map[string]interface{}{"x": kv(c[0], c[1])}
			})
			if got, ok := stringMap(namespaceNamed(stressObjs(t, res), "reporting"), "annotations")[c[0]]; ok && got == c[1] {
				t.Errorf("project annotation %s=%s applied to the project Namespace", c[0], c[1])
			}
		})
	}
}

// A catalog annotation and displayName both set openshift.io/display-name: one silently wins.
func TestStress_Annotations_FieldCollision(t *testing.T) {
	t.Parallel()
	res := stressProjectsInput(t, func(projects map[string]interface{}) {
		p := proj(projects, "reporting")
		p["displayName"] = "From Field"
		p["annotations"] = map[string]interface{}{"dn": kv("openshift.io/display-name", "from-catalog")}
	})
	objs := stressObjs(t, res)
	if onboardingStatus(t, objs)["reporting"] == "" {
		t.Errorf("displayName and a catalog annotation both set openshift.io/display-name; rendered %q with no report",
			stringMap(namespaceNamed(objs, "reporting"), "annotations")["openshift.io/display-name"])
	}
}
