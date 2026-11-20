//go:build integration

package resolver

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/auto-shift/autoshiftv2/tools/internal/labels"
)

const (
	customSCCsPolicy = "stable/custom-sccs"
	// PolicyGenerator names each ConfigurationPolicy after its Policy: policy-custom-sccs
	// (manifests/) and policy-custom-sccs-test (test/).
	sccEnforceCP = "policy-custom-sccs"
	sccInformCP  = "policy-custom-sccs-test"
	sccStatus    = "custom-sccs-status"
)

// runSCCs runs the pipeline for stable/custom-sccs only, against the shared testdata, with the
// example config.securityContextConstraints optionally mutated.
func runSCCs(t *testing.T, mutate func(sccs map[string]interface{})) ChartResult {
	t.Helper()
	return runSCCsConfig(t, func(cfg map[string]interface{}) {
		if mutate == nil {
			return
		}
		sccs, ok := cfg["securityContextConstraints"].(map[string]interface{})
		if !ok {
			t.Fatal("_example.yaml config.securityContextConstraints is missing")
		}
		mutate(sccs)
	})
}

// runSCCsConfig is runSCCs with access to the whole example hub config. Same flow as
// runOnboardingAll (project_onboarding_test.go), for a different policy.
func runSCCsConfig(t *testing.T, mutate func(cfg map[string]interface{})) ChartResult {
	t.Helper()
	root := repoRoot(t)
	valuesDir := filepath.Join(root, "autoshift", "values")
	testdataDir := filepath.Join(root, "tools", "testdata")

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
	only := func(policy string) bool { return policy == customSCCsPolicy }
	_, results, err := runPipelineTransform(filepath.Join(root, "policies"), only, cachedRender, nil, ctx, nil, r, spokeR, declared, &cloned, testdataDir)
	if err != nil {
		t.Fatalf("runPipeline: %v", err)
	}
	for _, res := range results {
		if res.Policy != customSCCsPolicy {
			continue
		}
		if res.Err != nil || !res.ResolveOK || len(res.SpokeWarns) > 0 || len(res.YAMLErrors) > 0 {
			t.Fatalf("%s did not resolve: err=%v warns=%v spoke=%v yaml=%v",
				res.Policy, res.Err, res.ResolveWarns, res.SpokeWarns, res.YAMLErrors)
		}
		return res
	}
	t.Fatalf("%s not found in pipeline results", customSCCsPolicy)
	return ChartResult{}
}

var (
	sccBaseOnce sync.Once
	sccBaseRes  ChartResult
)

func sccBase(t *testing.T) []onboardingObject {
	t.Helper()
	sccBaseOnce.Do(func() { sccBaseRes = runSCCs(t, nil) })
	return onboardingObjects(t, sccBaseRes)
}

// sccObject returns the object of kind/namespace/name rendered (not mustnothave) in configPolicy.
func sccObject(objs []onboardingObject, configPolicy, kind, namespace, name string) *onboardingObject {
	for i, o := range objs {
		meta, _ := o.obj["metadata"].(map[string]interface{})
		ns, _ := meta["namespace"].(string)
		if o.configPolicy == configPolicy && o.obj["kind"] == kind && ns == namespace &&
			meta["name"] == name && o.complianceType != "mustnothave" {
			return &objs[i]
		}
	}
	return nil
}

// sccStale returns the mustnothave entries per configPolicy, keyed kind|namespace|name, status
// ConfigMap excluded.
func sccStale(objs []onboardingObject, configPolicy string) map[string]bool {
	out := map[string]bool{}
	for _, o := range objs {
		meta, _ := o.obj["metadata"].(map[string]interface{})
		ns, _ := meta["namespace"].(string)
		name, _ := meta["name"].(string)
		if o.configPolicy == configPolicy && o.complianceType == "mustnothave" && name != sccStatus {
			out[o.obj["kind"].(string)+"|"+ns+"|"+name] = true
		}
	}
	return out
}

// sccStatusData returns the data of the status ConfigMap written by the enforce ConfigurationPolicy,
// or nil when the enforce ConfigurationPolicy asserts it absent.
func sccStatusData(t *testing.T, objs []onboardingObject) map[string]string {
	t.Helper()
	if o := sccObject(objs, sccEnforceCP, "ConfigMap", "open-cluster-management-agent-addon", sccStatus); o != nil {
		if o.complianceType != "mustonlyhave" {
			t.Errorf("status ConfigMap complianceType = %s, want mustonlyhave", o.complianceType)
		}
		return stringMap(o.obj, "data")
	}
	return nil
}

func strList(v interface{}) []string {
	items, _ := v.([]interface{})
	out := []string{}
	for _, i := range items {
		s, _ := i.(string)
		out = append(out, s)
	}
	return out
}

func TestCustomSCCs_TemplatesInSync(t *testing.T) {
	t.Parallel()
	assertTemplatesInSync(t, customSCCsPolicy, "sccs.yaml")
}

// The example entry sets only runAsUser and readOnlyRootFilesystem; everything else is nonroot-v2.
func TestCustomSCCs_DefaultsFromNonrootV2(t *testing.T) {
	t.Parallel()
	objs := sccBase(t)
	o := sccObject(objs, sccEnforceCP, "SecurityContextConstraints", "", "example-fixed-uid")
	if o == nil {
		t.Fatal("no SecurityContextConstraints example-fixed-uid in the enforce ConfigurationPolicy")
	}
	if o.complianceType != "mustonlyhave" {
		t.Errorf("complianceType = %s, want mustonlyhave", o.complianceType)
	}
	scc := o.obj
	for _, f := range []string{"allowHostDirVolumePlugin", "allowHostIPC", "allowHostNetwork", "allowHostPID",
		"allowHostPorts", "allowPrivilegeEscalation", "allowPrivilegedContainer"} {
		if scc[f] != false {
			t.Errorf("%s = %v, want false", f, scc[f])
		}
	}
	if scc["readOnlyRootFilesystem"] != true {
		t.Errorf("readOnlyRootFilesystem = %v, want the override true", scc["readOnlyRootFilesystem"])
	}
	want := map[string][]string{
		"allowedCapabilities":      {"NET_BIND_SERVICE"},
		"requiredDropCapabilities": {"ALL"},
		"defaultAddCapabilities":   {},
		"seccompProfiles":          {"runtime/default"},
		"volumes": {"configMap", "csi", "downwardAPI", "emptyDir", "ephemeral", "image",
			"persistentVolumeClaim", "projected", "secret"},
		"users":  {},
		"groups": {},
	}
	for f, w := range want {
		if got := strList(scc[f]); !reflect.DeepEqual(got, w) {
			t.Errorf("%s = %v, want %v", f, got, w)
		}
	}
	runAsUser, _ := scc["runAsUser"].(map[string]interface{})
	if runAsUser["type"] != "MustRunAsRange" || runAsUser["uidRangeMin"] != float64(1000) || runAsUser["uidRangeMax"] != float64(1999) {
		t.Errorf("runAsUser = %v, want the override MustRunAsRange 1000-1999", runAsUser)
	}
	for f, typ := range map[string]string{"seLinuxContext": "MustRunAs", "fsGroup": "RunAsAny", "supplementalGroups": "RunAsAny"} {
		if st, _ := scc[f].(map[string]interface{}); st["type"] != typ || len(st) != 1 {
			t.Errorf("%s = %v, want {type: %s}", f, scc[f], typ)
		}
	}
	// Every managed field is explicit, the way the API server returns it, so mustonlyhave converges
	// and a field dropped from the config is cleared on the live SCC.
	for _, f := range []string{"allowedUnsafeSysctls", "forbiddenSysctls", "allowedFlexVolumes"} {
		if v, ok := scc[f].([]interface{}); !ok || len(v) != 0 {
			t.Errorf("%s = %#v, want []", f, scc[f])
		}
	}
	for _, f := range []string{"priority", "defaultAllowPrivilegeEscalation"} {
		if v, ok := scc[f]; !ok || v != nil {
			t.Errorf("%s = %#v (present %v), want null", f, v, ok)
		}
	}
	// userNamespaceLevel is rendered only when set, so the policy applies where the schema lacks it.
	if v, ok := scc["userNamespaceLevel"]; ok {
		t.Errorf("userNamespaceLevel = %v, want it absent when the entry does not set it", v)
	}
	if l := stringMap(scc["metadata"].(map[string]interface{}), "labels"); l["autoshift.io/managed-by"] != "custom-sccs" || l["autoshift.io/scc"] != "example-fixed-uid" {
		t.Errorf("labels = %v", l)
	}
	if d := sccStatusData(t, objs); d != nil {
		t.Errorf("example config reports errors: %v", d)
	}
}

func TestCustomSCCs_RBAC(t *testing.T) {
	t.Parallel()
	objs := sccBase(t)
	cr := sccObject(objs, sccEnforceCP, "ClusterRole", "", "autoshift-scc-example-fixed-uid")
	if cr == nil {
		t.Fatal("no ClusterRole autoshift-scc-example-fixed-uid")
	}
	if cr.complianceType != "mustonlyhave" {
		t.Errorf("ClusterRole complianceType = %s, want mustonlyhave", cr.complianceType)
	}
	wantRules := []interface{}{map[string]interface{}{
		"apiGroups":     []interface{}{"security.openshift.io"},
		"resources":     []interface{}{"securitycontextconstraints"},
		"resourceNames": []interface{}{"example-fixed-uid"},
		"verbs":         []interface{}{"use"},
	}}
	if !reflect.DeepEqual(cr.obj["rules"], wantRules) {
		t.Errorf("ClusterRole rules = %v, want %v", cr.obj["rules"], wantRules)
	}
	rb := sccObject(objs, sccEnforceCP, "RoleBinding", "example-ci", "autoshift-scc-example-fixed-uid")
	if rb == nil {
		t.Fatal("no RoleBinding autoshift-scc-example-fixed-uid in example-ci")
	}
	if rb.complianceType != "mustonlyhave" {
		t.Errorf("RoleBinding complianceType = %s, want mustonlyhave", rb.complianceType)
	}
	wantRef := map[string]interface{}{"apiGroup": "rbac.authorization.k8s.io", "kind": "ClusterRole", "name": "autoshift-scc-example-fixed-uid"}
	if !reflect.DeepEqual(rb.obj["roleRef"], wantRef) {
		t.Errorf("roleRef = %v, want %v", rb.obj["roleRef"], wantRef)
	}
	// legacy-builder is false, so only builder is bound.
	wantSubjects := []interface{}{map[string]interface{}{"kind": "ServiceAccount", "name": "builder", "namespace": "example-ci"}}
	if !reflect.DeepEqual(rb.obj["subjects"], wantSubjects) {
		t.Errorf("subjects = %v, want %v", rb.obj["subjects"], wantSubjects)
	}
	// Nothing is rendered in the inform ConfigurationPolicy but stale reports and the status check.
	for _, o := range objs {
		if o.configPolicy == sccInformCP && o.complianceType != "mustnothave" {
			t.Errorf("inform ConfigurationPolicy renders %v %s", o.obj["kind"], o.complianceType)
		}
	}
}

// A rootless podman-in-pod posture: setuid helpers need privilege escalation and SETUID/SETGID.
func TestCustomSCCs_RootlessPodmanOverrides(t *testing.T) {
	t.Parallel()
	res := runSCCs(t, func(sccs map[string]interface{}) {
		sccs["example-rootless-podman"] = map[string]interface{}{
			"spec": map[string]interface{}{
				"allowPrivilegedContainer": false,
				"allowPrivilegeEscalation": true,
				"runAsUser":                map[string]interface{}{"type": "MustRunAsNonRoot"},
				"requiredDropCapabilities": []interface{}{"KILL", "MKNOD"},
				"allowedCapabilities":      []interface{}{"SETUID", "SETGID"},
				"seccompProfiles":          []interface{}{"runtime/default"},
			},
			"serviceAccounts": map[string]interface{}{"example-ci/podman-runner": true, "example-ci/builder": true},
		}
	})
	objs := onboardingObjects(t, res)
	o := sccObject(objs, sccEnforceCP, "SecurityContextConstraints", "", "example-rootless-podman")
	if o == nil {
		t.Fatal("no SCC example-rootless-podman")
	}
	if o.obj["allowPrivilegeEscalation"] != true || o.obj["allowPrivilegedContainer"] != false || o.obj["allowHostNetwork"] != false {
		t.Errorf("SCC = %v", o.obj)
	}
	if got := strList(o.obj["requiredDropCapabilities"]); !reflect.DeepEqual(got, []string{"KILL", "MKNOD"}) {
		t.Errorf("requiredDropCapabilities = %v, want the override [KILL MKNOD], not merged with ALL", got)
	}
	if got := strList(o.obj["allowedCapabilities"]); !reflect.DeepEqual(got, []string{"SETUID", "SETGID"}) {
		t.Errorf("allowedCapabilities = %v, want [SETUID SETGID]", got)
	}
	if got := strList(o.obj["volumes"]); len(got) != 9 {
		t.Errorf("volumes = %v, want the nonroot-v2 nine", got)
	}
	rb := sccObject(objs, sccEnforceCP, "RoleBinding", "example-ci", "autoshift-scc-example-rootless-podman")
	if rb == nil {
		t.Fatal("no RoleBinding for example-rootless-podman")
	}
	var names []string
	for _, s := range rb.obj["subjects"].([]interface{}) {
		names = append(names, s.(map[string]interface{})["name"].(string))
	}
	if !reflect.DeepEqual(names, []string{"builder", "podman-runner"}) {
		t.Errorf("subjects = %v, want [builder podman-runner]", names)
	}
	if d := sccStatusData(t, objs); d != nil {
		t.Errorf("unexpected errors: %v", d)
	}
}

func TestCustomSCCs_Rejections(t *testing.T) {
	t.Parallel()
	entry := func(spec map[string]interface{}, extra map[string]interface{}) map[string]interface{} {
		e := map[string]interface{}{"spec": spec, "serviceAccounts": map[string]interface{}{"example-ci/builder": true}}
		for k, v := range extra {
			e[k] = v
		}
		return e
	}
	cases := []struct {
		name  string
		key   string
		value interface{}
		want  string
	}{
		{"privileged container", "x-priv", entry(map[string]interface{}{"allowPrivilegedContainer": true}, nil), "spec.allowPrivilegedContainer: true needs allowPrivileged: true"},
		{"host network", "x-net", entry(map[string]interface{}{"allowHostNetwork": true}, nil), "spec.allowHostNetwork: true needs allowPrivileged"},
		{"host pid", "x-pid", entry(map[string]interface{}{"allowHostPID": true}, nil), "spec.allowHostPID: true needs"},
		{"host ipc", "x-ipc", entry(map[string]interface{}{"allowHostIPC": true}, nil), "spec.allowHostIPC: true needs"},
		{"host ports", "x-ports", entry(map[string]interface{}{"allowHostPorts": true}, nil), "spec.allowHostPorts: true needs"},
		{"hostPath", "x-hostpath", entry(map[string]interface{}{"allowHostDirVolumePlugin": true}, nil), "spec.allowHostDirVolumePlugin: true needs"},
		{"run as any", "x-any", entry(map[string]interface{}{"runAsUser": map[string]interface{}{"type": "RunAsAny"}}, nil), "spec.runAsUser.type RunAsAny needs allowPrivileged"},
		{"selinux run as any", "x-selinux", entry(map[string]interface{}{"seLinuxContext": map[string]interface{}{"type": "RunAsAny"}}, nil), "spec.seLinuxContext.type RunAsAny needs allowPrivileged"},
		{"hostPath volume", "x-vol-hostpath", entry(map[string]interface{}{"volumes": []interface{}{"secret", "hostPath"}}, nil), "spec.volumes hostPath needs allowPrivileged: true"},
		{"wildcard volume", "x-vol-any", entry(map[string]interface{}{"volumes": []interface{}{"*"}}, nil), "spec.volumes * needs allowPrivileged"},
		{"wildcard capability", "x-cap-any", entry(map[string]interface{}{"allowedCapabilities": []interface{}{"*"}}, nil), "spec.allowedCapabilities * needs allowPrivileged"},
		{"ALL capability", "x-cap-all", entry(map[string]interface{}{"allowedCapabilities": []interface{}{"ALL"}}, nil), "spec.allowedCapabilities ALL needs allowPrivileged"},
		{"SYS_ADMIN capability", "x-cap-admin", entry(map[string]interface{}{"allowedCapabilities": []interface{}{"SETUID", "SYS_ADMIN"}}, nil), "spec.allowedCapabilities SYS_ADMIN needs allowPrivileged"},
		{"CAP_ prefix lower case", "x-cap-prefix", entry(map[string]interface{}{"allowedCapabilities": []interface{}{"cap_net_raw"}}, nil), "spec.allowedCapabilities cap_net_raw needs allowPrivileged"},
		{"default-added capability", "x-cap-default", entry(map[string]interface{}{"defaultAddCapabilities": []interface{}{"NET_ADMIN"}}, nil), "spec.defaultAddCapabilities NET_ADMIN needs allowPrivileged"},
		{"unsafe sysctls", "x-sysctl", entry(map[string]interface{}{"allowedUnsafeSysctls": []interface{}{"kernel.msg*"}}, nil), "spec.allowedUnsafeSysctls needs allowPrivileged"},
		{"uid 0", "x-uid0", entry(map[string]interface{}{"runAsUser": map[string]interface{}{"type": "MustRunAs", "uid": 0}}, nil), "spec.runAsUser.uid 0 needs allowPrivileged: true"},
		{"uid negative", "x-uidneg", entry(map[string]interface{}{"runAsUser": map[string]interface{}{"type": "MustRunAs", "uid": -1}}, nil), "spec.runAsUser.uid -1 needs allowPrivileged"},
		{"uid range from 0", "x-uidrange0", entry(map[string]interface{}{"runAsUser": map[string]interface{}{"type": "MustRunAsRange", "uidRangeMin": 0, "uidRangeMax": 999}}, nil), "spec.runAsUser.uidRangeMin 0 needs allowPrivileged: true"},
		{"selinux spc_t", "x-spc", entry(map[string]interface{}{"seLinuxContext": map[string]interface{}{"type": "MustRunAs", "seLinuxOptions": map[string]interface{}{"type": "spc_t"}}}, nil), "spec.seLinuxContext.seLinuxOptions.type spc_t needs allowPrivileged: true"},
		{"selinux unconfined_t upper case", "x-unconfined-t", entry(map[string]interface{}{"seLinuxContext": map[string]interface{}{"type": "MustRunAs", "seLinuxOptions": map[string]interface{}{"type": "UNCONFINED_T", "level": "s0"}}}, nil), "spec.seLinuxContext.seLinuxOptions.type UNCONFINED_T needs allowPrivileged"},
		{"seccomp wildcard", "x-seccomp-any", entry(map[string]interface{}{"seccompProfiles": []interface{}{"runtime/default", "*"}}, nil), "spec.seccompProfiles * needs allowPrivileged: true"},
		{"seccomp unconfined mixed case", "x-seccomp-unconfined", entry(map[string]interface{}{"seccompProfiles": []interface{}{"Unconfined"}}, nil), "spec.seccompProfiles Unconfined needs allowPrivileged"},
		{"users", "x-users", entry(map[string]interface{}{"users": []interface{}{"system:admin"}}, nil), "spec.users is not allowed"},
		{"groups", "x-groups", entry(map[string]interface{}{"groups": []interface{}{"system:authenticated"}}, nil), "spec.groups is not allowed"},
		{"invalid name", "Bad_Name", entry(map[string]interface{}{}, nil), "name is not a valid DNS-1123 label"},
		{"reserved name", "restricted-v2", entry(map[string]interface{}{}, nil), "built-in OpenShift SCC"},
		{"unknown spec field", "x-field", entry(map[string]interface{}{"allowEverything": true}, nil), "spec.allowEverything is not a SecurityContextConstraints field"},
		{"capabilities not a list", "x-caps", entry(map[string]interface{}{"allowedCapabilities": "SETUID"}, nil), "spec.allowedCapabilities is not a list of strings"},
		{"drop not a list", "x-drop", entry(map[string]interface{}{"requiredDropCapabilities": map[string]interface{}{"KILL": true}}, nil), "spec.requiredDropCapabilities is not a list of strings"},
		{"list of non-strings", "x-seccomp", entry(map[string]interface{}{"seccompProfiles": []interface{}{1}}, nil), "spec.seccompProfiles is not a list of strings"},
		{"bool not bool", "x-bool", entry(map[string]interface{}{"readOnlyRootFilesystem": "yes"}, nil), "spec.readOnlyRootFilesystem is not true or false"},
		{"strategy type", "x-type", entry(map[string]interface{}{"runAsUser": map[string]interface{}{"type": "Whatever"}}, nil), "spec.runAsUser.type is not one of"},
		{"strategy key", "x-stkey", entry(map[string]interface{}{"seLinuxContext": map[string]interface{}{"type": "MustRunAs", "uid": 1}}, nil), "spec.seLinuxContext.uid is not one of"},
		{"strategy not map", "x-stmap", entry(map[string]interface{}{"fsGroup": "RunAsAny"}, nil), "spec.fsGroup is not a map"},
		{"priority", "x-prio", entry(map[string]interface{}{"priority": -1}, nil), "spec.priority is not a non-negative integer"},
		{"userNamespaceLevel", "x-userns", entry(map[string]interface{}{"userNamespaceLevel": "Sometimes"}, nil), "spec.userNamespaceLevel is not"},
		{"spec not map", "x-spec", entry(nil, map[string]interface{}{"spec": []interface{}{"a"}}), "spec is not a map"},
		{"entry not map", "x-entry", "yes", "entry is not a map"},
		{"unknown entry key", "x-key", entry(map[string]interface{}{}, map[string]interface{}{"users": []interface{}{"a"}}), "key users is not one of"},
		{"enabled not bool", "x-enabled", entry(map[string]interface{}{}, map[string]interface{}{"enabled": "maybe"}), "enabled is not true or false"},
		{"ack not bool", "x-ack", entry(map[string]interface{}{}, map[string]interface{}{"allowPrivileged": "sure"}), "allowPrivileged is not true or false"},
		{"sa no namespace", "x-sa1", entry(map[string]interface{}{}, map[string]interface{}{"serviceAccounts": map[string]interface{}{"builder": true}}), "serviceAccounts.builder is not <namespace>/<serviceaccount>"},
		{"sa three parts", "x-sa2", entry(map[string]interface{}{}, map[string]interface{}{"serviceAccounts": map[string]interface{}{"a/b/c": true}}), "serviceAccounts.a/b/c is not <namespace>/<serviceaccount>"},
		{"sa bad namespace", "x-sa3", entry(map[string]interface{}{}, map[string]interface{}{"serviceAccounts": map[string]interface{}{"Team_A/builder": true}}), "is not <namespace>/<serviceaccount>"},
		{"sa value", "x-sa4", entry(map[string]interface{}{}, map[string]interface{}{"serviceAccounts": map[string]interface{}{"example-ci/builder": "yes"}}), "serviceAccounts.example-ci/builder is not true or false"},
		{"sa list", "x-sa5", entry(map[string]interface{}{}, map[string]interface{}{"serviceAccounts": []interface{}{"example-ci/builder"}}), "serviceAccounts is not a map"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := runSCCs(t, func(sccs map[string]interface{}) { sccs[tc.key] = tc.value })
			objs := onboardingObjects(t, res)
			statusKey := strings.ReplaceAll(tc.key, "/", "_")
			data := sccStatusData(t, objs)
			if !strings.Contains(data[statusKey], tc.want) {
				t.Errorf("status[%s] = %q, want it to contain %q (all: %v)", statusKey, data[statusKey], tc.want, data)
			}
			if o := sccObject(objs, sccEnforceCP, "SecurityContextConstraints", "", tc.key); o != nil {
				t.Errorf("rejected entry rendered an SCC")
			}
			if o := sccObject(objs, sccEnforceCP, "ClusterRole", "", "autoshift-scc-"+tc.key); o != nil {
				t.Errorf("rejected entry rendered a ClusterRole")
			}
			if sccObject(objs, sccEnforceCP, "SecurityContextConstraints", "", "example-fixed-uid") == nil {
				t.Errorf("a bad entry blocked the valid example-fixed-uid")
			}
			// The inform ConfigurationPolicy asserts the status ConfigMap is absent.
			found := false
			for _, o := range objs {
				meta, _ := o.obj["metadata"].(map[string]interface{})
				if o.configPolicy == sccInformCP && meta["name"] == sccStatus && o.complianceType == "mustnothave" {
					found = true
				}
			}
			if !found {
				t.Error("inform ConfigurationPolicy does not assert the status ConfigMap is absent")
			}
		})
	}
}

// Every capability in the template's $privCaps is gated, in both capability fields, while
// SYS_CHROOT and the rootless podman pair are not.
func TestCustomSCCs_PrivilegedCapabilities(t *testing.T) {
	t.Parallel()
	privCaps := []string{"*", "ALL", "SYS_ADMIN", "SYS_MODULE", "SYS_PTRACE", "SYS_RAWIO", "SYS_BOOT", "SYS_TIME",
		"NET_ADMIN", "NET_RAW", "BPF", "PERFMON", "DAC_READ_SEARCH", "MAC_ADMIN", "MAC_OVERRIDE", "SYSLOG"}
	name := func(field string, i int) string {
		return "x-" + field + "-" + strings.ToLower(strings.Repeat("c", i+1))
	}
	res := runSCCs(t, func(sccs map[string]interface{}) {
		for i, c := range privCaps {
			for _, f := range []string{"allowedCapabilities", "defaultAddCapabilities"} {
				sccs[name(strings.ToLower(f[:7]), i)] = map[string]interface{}{"spec": map[string]interface{}{f: []interface{}{c}}}
			}
		}
		sccs["x-chroot"] = map[string]interface{}{"spec": map[string]interface{}{
			"allowedCapabilities": []interface{}{"SETUID", "SETGID", "SYS_CHROOT"}}}
	})
	objs := onboardingObjects(t, res)
	data := sccStatusData(t, objs)
	for i, c := range privCaps {
		for _, f := range []string{"allowedCapabilities", "defaultAddCapabilities"} {
			n := name(strings.ToLower(f[:7]), i)
			if want := "spec." + f + " " + c + " needs allowPrivileged"; !strings.Contains(data[n], want) {
				t.Errorf("status[%s] = %q, want %q", n, data[n], want)
			}
			if sccObject(objs, sccEnforceCP, "SecurityContextConstraints", "", n) != nil {
				t.Errorf("%s with %s rendered without allowPrivileged", f, c)
			}
		}
	}
	if sccObject(objs, sccEnforceCP, "SecurityContextConstraints", "", "x-chroot") == nil {
		t.Errorf("SETUID, SETGID, SYS_CHROOT rejected: %q", data["x-chroot"])
	}
}

// allowPrivileged: true is the escape hatch: the gated settings are then rendered as given.
func TestCustomSCCs_AllowPrivilegedAcknowledged(t *testing.T) {
	t.Parallel()
	res := runSCCs(t, func(sccs map[string]interface{}) {
		sccs["example-node-agent"] = map[string]interface{}{
			"allowPrivileged": true,
			"spec": map[string]interface{}{
				"allowPrivilegedContainer": true,
				"allowHostNetwork":         true,
				"allowHostDirVolumePlugin": true,
				"runAsUser":                map[string]interface{}{"type": "RunAsAny"},
				"seLinuxContext":           map[string]interface{}{"type": "RunAsAny"},
				"volumes":                  []interface{}{"hostPath", "secret"},
				"allowedCapabilities":      []interface{}{"SYS_ADMIN", "NET_RAW"},
				"defaultAddCapabilities":   []interface{}{"NET_ADMIN"},
				"allowedUnsafeSysctls":     []interface{}{"net.*"},
				"priority":                 10,
				"userNamespaceLevel":       "AllowHostLevel",
			},
		}
		sccs["example-root-agent"] = map[string]interface{}{
			"allowPrivileged": true,
			"spec": map[string]interface{}{
				"runAsUser":       map[string]interface{}{"type": "MustRunAs", "uid": 0},
				"seLinuxContext":  map[string]interface{}{"type": "MustRunAs", "seLinuxOptions": map[string]interface{}{"type": "spc_t"}},
				"seccompProfiles": []interface{}{"*", "unconfined"},
			},
		}
		sccs["example-range-agent"] = map[string]interface{}{
			"allowPrivileged": true,
			"spec": map[string]interface{}{
				"runAsUser": map[string]interface{}{"type": "MustRunAsRange", "uidRangeMin": 0, "uidRangeMax": 999},
			},
		}
	})
	objs := onboardingObjects(t, res)
	o := sccObject(objs, sccEnforceCP, "SecurityContextConstraints", "", "example-node-agent")
	if o == nil {
		t.Fatalf("acknowledged entry not rendered; status: %v", sccStatusData(t, objs))
	}
	if d := sccStatusData(t, objs); d != nil {
		t.Errorf("acknowledged entry reported: %v", d)
	}
	if o.obj["allowPrivilegedContainer"] != true || o.obj["allowHostNetwork"] != true || o.obj["priority"] != float64(10) || o.obj["userNamespaceLevel"] != "AllowHostLevel" {
		t.Errorf("SCC = %v", o.obj)
	}
	for f, w := range map[string][]string{"volumes": {"hostPath", "secret"}, "allowedCapabilities": {"SYS_ADMIN", "NET_RAW"},
		"defaultAddCapabilities": {"NET_ADMIN"}, "allowedUnsafeSysctls": {"net.*"}} {
		if got := strList(o.obj[f]); !reflect.DeepEqual(got, w) {
			t.Errorf("%s = %v, want %v", f, got, w)
		}
	}
	if st, _ := o.obj["seLinuxContext"].(map[string]interface{}); st["type"] != "RunAsAny" {
		t.Errorf("seLinuxContext = %v", o.obj["seLinuxContext"])
	}
	root := sccObject(objs, sccEnforceCP, "SecurityContextConstraints", "", "example-root-agent")
	if root == nil {
		t.Fatal("acknowledged example-root-agent not rendered")
	}
	if ru, _ := root.obj["runAsUser"].(map[string]interface{}); ru["type"] != "MustRunAs" || ru["uid"] != float64(0) {
		t.Errorf("runAsUser = %v, want MustRunAs uid 0", root.obj["runAsUser"])
	}
	if se, _ := root.obj["seLinuxContext"].(map[string]interface{}); !reflect.DeepEqual(se["seLinuxOptions"], map[string]interface{}{"type": "spc_t"}) {
		t.Errorf("seLinuxContext = %v, want seLinuxOptions.type spc_t", root.obj["seLinuxContext"])
	}
	if got := strList(root.obj["seccompProfiles"]); !reflect.DeepEqual(got, []string{"*", "unconfined"}) {
		t.Errorf("seccompProfiles = %v, want [* unconfined]", got)
	}
	rng := sccObject(objs, sccEnforceCP, "SecurityContextConstraints", "", "example-range-agent")
	if rng == nil {
		t.Fatal("acknowledged example-range-agent not rendered")
	}
	if ru, _ := rng.obj["runAsUser"].(map[string]interface{}); ru["uidRangeMin"] != float64(0) {
		t.Errorf("runAsUser = %v, want uidRangeMin 0", rng.obj["runAsUser"])
	}
	// No serviceAccounts: the ClusterRole renders, no RoleBinding.
	if sccObject(objs, sccEnforceCP, "ClusterRole", "", "autoshift-scc-example-node-agent") == nil {
		t.Error("no ClusterRole")
	}
	for _, x := range objs {
		meta, _ := x.obj["metadata"].(map[string]interface{})
		if x.obj["kind"] == "RoleBinding" && meta["name"] == "autoshift-scc-example-node-agent" {
			t.Errorf("RoleBinding rendered without serviceAccounts: %v", x.obj)
		}
	}
}

// Non-root UIDs, an unset uid or uidRangeMin (SCC admission fills it from the namespace's
// openshift.io/sa.scc.uid-range annotation), a confined SELinux type and runtime/default seccomp need
// no acknowledgment.
func TestCustomSCCs_NonRootStrategiesNotGated(t *testing.T) {
	t.Parallel()
	specs := map[string]map[string]interface{}{
		"x-uid":       {"runAsUser": map[string]interface{}{"type": "MustRunAs", "uid": 1000}},
		"x-uid-unset": {"runAsUser": map[string]interface{}{"type": "MustRunAs"}},
		"x-range":     {"runAsUser": map[string]interface{}{"type": "MustRunAsRange", "uidRangeMin": 10, "uidRangeMax": 20}},
		"x-range-ns":  {"runAsUser": map[string]interface{}{"type": "MustRunAsRange"}},
		"x-selinux":   {"seLinuxContext": map[string]interface{}{"type": "MustRunAs", "seLinuxOptions": map[string]interface{}{"type": "container_t", "level": "s0:c1,c2"}}},
		"x-seccomp":   {"seccompProfiles": []interface{}{"runtime/default", "localhost/example.json"}},
	}
	res := runSCCs(t, func(sccs map[string]interface{}) {
		for n, s := range specs {
			sccs[n] = map[string]interface{}{"spec": s}
		}
	})
	objs := onboardingObjects(t, res)
	data := sccStatusData(t, objs)
	for n := range specs {
		if sccObject(objs, sccEnforceCP, "SecurityContextConstraints", "", n) == nil {
			t.Errorf("%s rejected: %q", n, data[n])
		}
	}
}

// Two names that sanitize to the same status key get distinct keys, so neither error is hidden.
func TestCustomSCCs_StatusKeyCollision(t *testing.T) {
	t.Parallel()
	res := runSCCs(t, func(sccs map[string]interface{}) {
		sccs["bad#name"] = map[string]interface{}{}
		sccs["bad@name"] = map[string]interface{}{}
	})
	data := sccStatusData(t, onboardingObjects(t, res))
	for _, k := range []string{"bad_name", "bad_name-2"} {
		if !strings.Contains(data[k], "name is not a valid DNS-1123 label") {
			t.Errorf("status[%s] = %q (all: %v)", k, data[k], data)
		}
	}
}

// Template delimiters inside config strings reach the SCC as literal text and are never evaluated.
func TestCustomSCCs_TemplateDelimitersNotInjected(t *testing.T) {
	t.Parallel()
	caps := []interface{}{`{{ fail "spoke" }}`, `{{hub .ManagedClusterName hub}}`}
	vols := []interface{}{"secret", `{{ (lookup "v1" "Namespace" "" "example-ci").metadata.name }}`}
	res := runSCCs(t, func(sccs map[string]interface{}) {
		sccs["example-delims"] = map[string]interface{}{"spec": map[string]interface{}{
			"allowedCapabilities": caps, "volumes": vols}}
	})
	objs := onboardingObjects(t, res)
	o := sccObject(objs, sccEnforceCP, "SecurityContextConstraints", "", "example-delims")
	if o == nil {
		t.Fatalf("entry not rendered; status: %v", sccStatusData(t, objs))
	}
	if got := strList(o.obj["allowedCapabilities"]); !reflect.DeepEqual(got, strList(caps)) {
		t.Errorf("allowedCapabilities = %q, want the literal %q", got, strList(caps))
	}
	if got := strList(o.obj["volumes"]); !reflect.DeepEqual(got, strList(vols)) {
		t.Errorf("volumes = %q, want the literal %q", got, strList(vols))
	}
}

// An SCC of the same name that custom-sccs does not own is never taken over.
func TestCustomSCCs_ForeignSCCNotTakenOver(t *testing.T) {
	t.Parallel()
	res := runSCCs(t, func(sccs map[string]interface{}) {
		sccs["example-operator-scc"] = map[string]interface{}{"serviceAccounts": map[string]interface{}{"example-ci/builder": true}}
	})
	objs := onboardingObjects(t, res)
	if sccObject(objs, sccEnforceCP, "SecurityContextConstraints", "", "example-operator-scc") != nil {
		t.Error("foreign SCC example-operator-scc was rendered")
	}
	if sccObject(objs, sccEnforceCP, "RoleBinding", "example-ci", "autoshift-scc-example-operator-scc") != nil {
		t.Error("RoleBinding rendered for a blocked entry")
	}
	if got := sccStatusData(t, objs)["example-operator-scc"]; !strings.Contains(got, "already exists and is not managed by custom-sccs") {
		t.Errorf("status = %q", got)
	}
}

// A namespace that does not exist skips only its RoleBinding.
func TestCustomSCCs_MissingNamespace(t *testing.T) {
	t.Parallel()
	res := runSCCs(t, func(sccs map[string]interface{}) {
		e := sccs["example-fixed-uid"].(map[string]interface{})
		e["serviceAccounts"].(map[string]interface{})["example-missing/builder"] = true
	})
	objs := onboardingObjects(t, res)
	if sccObject(objs, sccEnforceCP, "SecurityContextConstraints", "", "example-fixed-uid") == nil {
		t.Error("SCC not rendered")
	}
	if sccObject(objs, sccEnforceCP, "RoleBinding", "example-ci", "autoshift-scc-example-fixed-uid") == nil {
		t.Error("RoleBinding in the existing namespace not rendered")
	}
	if sccObject(objs, sccEnforceCP, "RoleBinding", "example-missing", "autoshift-scc-example-fixed-uid") != nil {
		t.Error("RoleBinding rendered in a missing namespace")
	}
	if got := sccStatusData(t, objs)["example-fixed-uid"]; !strings.Contains(got, "namespace example-missing does not exist") {
		t.Errorf("status = %q", got)
	}
}

func TestCustomSCCs_TopLevelNotAMap(t *testing.T) {
	t.Parallel()
	res := runSCCsConfig(t, func(cfg map[string]interface{}) {
		cfg["securityContextConstraints"] = []interface{}{"example"}
	})
	objs := onboardingObjects(t, res)
	if got := sccStatusData(t, objs)["securityContextConstraints"]; got != "config.securityContextConstraints is not a map" {
		t.Errorf("status = %q", got)
	}
	if s := sccStale(objs, sccInformCP); len(s) != 0 {
		t.Errorf("sweep ran on unreadable config: %v", s)
	}
}

// Undeclared owned objects are reported by the inform ConfigurationPolicy and never deleted.
func TestCustomSCCs_StaleReported(t *testing.T) {
	t.Parallel()
	objs := sccBase(t)
	want := map[string]bool{
		"SecurityContextConstraints||example-retired":             true,
		"ClusterRole||autoshift-scc-example-retired":              true,
		"RoleBinding|example-old|autoshift-scc-example-fixed-uid": true,
	}
	if got := sccStale(objs, sccInformCP); !reflect.DeepEqual(got, want) {
		t.Errorf("inform stale = %v, want %v", got, want)
	}
	if got := sccStale(objs, sccEnforceCP); len(got) != 0 {
		t.Errorf("enforce ConfigurationPolicy would delete %v", got)
	}
	for _, o := range objs {
		if o.configPolicy == sccInformCP && o.remediationAction != "inform" {
			t.Errorf("stale reports are in a %s ConfigurationPolicy", o.remediationAction)
		}
	}
}

// Objects of an entry with errors are held, not reported; a disabled entry's are reported.
func TestCustomSCCs_StaleHeldAndDisabled(t *testing.T) {
	t.Parallel()
	held := runSCCs(t, func(sccs map[string]interface{}) {
		sccs["example-fixed-uid"].(map[string]interface{})["spec"].(map[string]interface{})["allowHostPID"] = true
	})
	got := sccStale(onboardingObjects(t, held), sccInformCP)
	if got["RoleBinding|example-old|autoshift-scc-example-fixed-uid"] {
		t.Error("object of an entry with errors reported as stale")
	}
	if !got["SecurityContextConstraints||example-retired"] {
		t.Error("held entry stopped the rest of the sweep")
	}

	disabled := runSCCs(t, func(sccs map[string]interface{}) {
		sccs["example-fixed-uid"].(map[string]interface{})["enabled"] = false
		sccs["example-retired"] = map[string]interface{}{"enabled": false}
	})
	objs := onboardingObjects(t, disabled)
	if sccObject(objs, sccEnforceCP, "SecurityContextConstraints", "", "example-fixed-uid") != nil {
		t.Error("disabled entry rendered")
	}
	got = sccStale(objs, sccInformCP)
	for _, k := range []string{"SecurityContextConstraints||example-retired", "RoleBinding|example-old|autoshift-scc-example-fixed-uid"} {
		if !got[k] {
			t.Errorf("%s of a disabled entry not reported (got %v)", k, got)
		}
	}
	if d := sccStatusData(t, objs); d != nil {
		t.Errorf("disabled entries are not errors: %v", d)
	}
}

// A foreign RoleBinding autoshift-scc-<name> (no managed-by label) in one listed namespace skips
// only that RoleBinding and is reported; the SCC, the ClusterRole and the other namespaces render.
func TestCustomSCCs_ForeignRoleBindingSkipped(t *testing.T) {
	t.Parallel()
	foreign := `apiVersion: v1
kind: Namespace
metadata:
  name: example-other
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: autoshift-scc-example-rb
  namespace: example-ci
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: view
subjects:
  - kind: User
    name: alice
`
	objs := stressSCCs(t, foreign, func(sccs map[string]interface{}) {
		sccs["example-rb"] = map[string]interface{}{"serviceAccounts": map[string]interface{}{
			"example-ci/builder": true, "example-other/builder": true}}
	})
	if sccObject(objs, sccEnforceCP, "RoleBinding", "example-ci", "autoshift-scc-example-rb") != nil {
		t.Error("foreign RoleBinding example-ci/autoshift-scc-example-rb taken over")
	}
	for _, o := range [][3]string{{"SecurityContextConstraints", "", "example-rb"}, {"ClusterRole", "", "autoshift-scc-example-rb"},
		{"RoleBinding", "example-other", "autoshift-scc-example-rb"}} {
		if sccObject(objs, sccEnforceCP, o[0], o[1], o[2]) == nil {
			t.Errorf("%s %s/%s not rendered", o[0], o[1], o[2])
		}
	}
	if got := sccStatusData(t, objs)["example-rb"]; !strings.Contains(got, "RoleBinding example-ci/autoshift-scc-example-rb already exists and is not managed by custom-sccs") {
		t.Errorf("status = %q", got)
	}
}

// MCS levels that keep separation render without allowPrivileged: one or two categories (a pod's
// own pair, in any order or as a two-category range), and no level at all. Any range, a level with
// no categories, and more than two categories are gated, because such a level dominates the
// labels of other pods.
func TestCustomSCCs_MCSLevelsKeepingSeparationAllowed(t *testing.T) {
	t.Parallel()
	levels := map[string]string{"example-mcs-a": "s0:c26,c5", "example-mcs-b": "s0:c5", "example-mcs-c": "s0:c1.c2", "example-mcs-d": "", "example-mcs-e": "S0:C26,C5"}
	objs := stressSCCs(t, "", func(sccs map[string]interface{}) {
		for n, lvl := range levels {
			sccs[n] = sccEntry(map[string]interface{}{"seLinuxContext": map[string]interface{}{
				"type": "MustRunAs", "seLinuxOptions": map[string]interface{}{"level": lvl}}})
		}
	})
	data := sccStatusData(t, objs)
	for n, lvl := range levels {
		if sccObject(objs, sccEnforceCP, "SecurityContextConstraints", "", n) == nil {
			t.Errorf("level %q rejected: %q", lvl, data[n])
		}
	}
	gated := map[string]string{"x-mcs-sys": "SystemHigh", "x-mcs-split": "s0:c0.c511,c512.c1023", "x-mcs-nocat": "s0-s0", "x-mcs-case": " S0 ",
		"x-mcs-half": "s0:c0.c511", "x-mcs-1022": "s0:c0.c1022", "x-mcs-range": "s0-s0:c1,c2", "x-mcs-three": "s0:c1,c2,c3",
		"x-mcs-span": "s0:c1.c10,c20", "x-mcs-range-wide": "s0-s0:c0.c1022"}
	objs = stressSCCs(t, "", func(sccs map[string]interface{}) {
		for n, lvl := range gated {
			sccs[n] = sccEntry(map[string]interface{}{"seLinuxContext": map[string]interface{}{
				"type": "MustRunAs", "seLinuxOptions": map[string]interface{}{"level": lvl}}})
		}
	})
	assertGated(t, objs, gated)
}
