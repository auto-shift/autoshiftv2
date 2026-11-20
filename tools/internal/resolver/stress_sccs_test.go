//go:build integration

package resolver

// Adversarial input for stable/custom-sccs. Each test asserts the contract of the policy README
// (and its stated intent: a privileged posture needs allowPrivileged: true, use is granted only to
// the listed ServiceAccounts, nothing is ever deleted, a foreign object is never taken over). A
// failing TestStress_SCC_<Bug> means the policy breaks that contract.

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/auto-shift/autoshiftv2/tools/internal/labels"
	sigsyaml "sigs.k8s.io/yaml"
)

// stressSCCs runs stable/custom-sccs with the example config mutated, against tools/testdata plus
// extra as one more stub file ("" for none).
func stressSCCs(t *testing.T, extra string, mutate func(sccs map[string]interface{})) []onboardingObject {
	t.Helper()
	root := repoRoot(t)
	valuesDir := filepath.Join(root, "autoshift", "values")
	testdataDir := filepath.Join(root, "tools", "testdata")
	if extra != "" {
		testdataDir = withStubs(t, extra)
	}
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
		mutate(cloned.HubConfig["securityContextConstraints"].(map[string]interface{}))
	}
	declared, err := labels.ExtractDeclaredFromTree(valuesDir, false)
	if err != nil {
		t.Fatalf("ExtractDeclaredFromTree: %v", err)
	}
	ctx := HubContext{ManagedClusterName: "lint-cluster", ManagedClusterLabels: BuildSyntheticLabels(declared)}
	cms, err := GenerateSyntheticConfigMaps(&cloned, ctx.ManagedClusterName, "policies-autoshift")
	if err != nil {
		t.Fatalf("GenerateSyntheticConfigMaps: %v", err)
	}
	stubs, _ := LoadTestResources(testdataDir)
	seed := stressDedupeSeed(append(cms, stubs...))
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
			t.Fatalf("custom-sccs did not resolve: err=%v warns=%v spoke=%v yaml=%v",
				res.Err, res.ResolveWarns, res.SpokeWarns, res.YAMLErrors)
		}
		return onboardingObjects(t, res)
	}
	t.Fatal("custom-sccs not in pipeline results")
	return nil
}

func sccEntry(spec map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{"spec": spec, "serviceAccounts": map[string]interface{}{"example-ci/builder": true}}
}

// assertGated: each named entry (no allowPrivileged) must render no SCC and report an error.
func assertGated(t *testing.T, objs []onboardingObject, names map[string]string) {
	t.Helper()
	data := sccStatusData(t, objs)
	for name, why := range names {
		if o := sccObject(objs, sccEnforceCP, "SecurityContextConstraints", "", name); o != nil {
			t.Errorf("%s (%s) rendered an SCC without allowPrivileged: true; status=%q", name, why, data[name])
		}
		if data[name] == "" {
			t.Errorf("%s (%s) not reported in custom-sccs-status", name, why)
		}
	}
}

// ---------------------------------------------------------------------------------------------
// Safety-gate bypasses: privileged postures reachable without allowPrivileged: true.
// ---------------------------------------------------------------------------------------------

// An empty seccompProfiles list means "no profile may be set" (OpenShift SCC API), so the pod runs
// with no seccomp filter from the SCC: the same outcome as the gated "unconfined", without the
// acknowledgment. nonroot-v2 and restricted-v2 require runtime/default for exactly this reason.
func TestStress_SCC_SeccompEmptyListBypassesGate(t *testing.T) {
	t.Parallel()
	objs := stressSCCs(t, "", func(sccs map[string]interface{}) {
		sccs["x-seccomp-empty"] = sccEntry(map[string]interface{}{"seccompProfiles": []interface{}{}})
	})
	assertGated(t, objs, map[string]string{"x-seccomp-empty": "seccompProfiles: [] (no seccomp)"})
}

// $privSELinux is a two-item denylist. container_runtime_t (the CRI-O domain), unconfined_service_t
// and kernel_t are unconfined domains in the RHCOS policy, as privileged as spc_t.
func TestStress_SCC_UnconfinedSELinuxDomainsBypassGate(t *testing.T) {
	t.Parallel()
	types := map[string]string{
		"x-se-runtime":   "container_runtime_t",
		"x-se-unconfsvc": "unconfined_service_t",
		"x-se-kernel":    "kernel_t",
		"x-se-init":      "init_t",
	}
	objs := stressSCCs(t, "", func(sccs map[string]interface{}) {
		for n, typ := range types {
			sccs[n] = sccEntry(map[string]interface{}{"seLinuxContext": map[string]interface{}{
				"type": "MustRunAs", "seLinuxOptions": map[string]interface{}{"type": typ}}})
		}
	})
	assertGated(t, objs, types)
}

// MustRunAs with seLinuxOptions.level "s0" (no categories) or the full range s0-s0:c0.c1023 drops
// the per-namespace MCS separation between containers. Not gated.
func TestStress_SCC_MCSLevelRemovesIsolationUngated(t *testing.T) {
	t.Parallel()
	levels := map[string]string{"x-mcs-none": "s0", "x-mcs-all": "s0-s0:c0.c1023"}
	objs := stressSCCs(t, "", func(sccs map[string]interface{}) {
		for n, lvl := range levels {
			sccs[n] = sccEntry(map[string]interface{}{"seLinuxContext": map[string]interface{}{
				"type": "MustRunAs", "seLinuxOptions": map[string]interface{}{"type": "container_t", "level": lvl}}})
		}
	})
	assertGated(t, objs, levels)
}

// Host-affecting capabilities that $privCaps does not list: AUDIT_CONTROL (kernel audit rules),
// SYS_RESOURCE, SYS_PACCT, SYS_NICE, LINUX_IMMUTABLE, SETFCAP, CHECKPOINT_RESTORE, IPC_LOCK,
// BLOCK_SUSPEND, WAKE_ALARM, SYS_TTY_CONFIG.
func TestStress_SCC_HostCapabilitiesUngated(t *testing.T) {
	t.Parallel()
	caps := []string{"AUDIT_CONTROL", "SYS_RESOURCE", "SYS_PACCT", "SYS_NICE", "LINUX_IMMUTABLE", "SETFCAP",
		"CHECKPOINT_RESTORE", "IPC_LOCK", "BLOCK_SUSPEND", "WAKE_ALARM", "SYS_TTY_CONFIG"}
	names := map[string]string{}
	objs := stressSCCs(t, "", func(sccs map[string]interface{}) {
		for _, c := range caps {
			n := "x-cap-" + strings.ReplaceAll(strings.ToLower(c), "_", "-")
			names[n] = c
			sccs[n] = sccEntry(map[string]interface{}{"allowedCapabilities": []interface{}{c}})
		}
	})
	assertGated(t, objs, names)
}

// Volume types that reach outside the pod sandbox without hostPath: flexVolume with an empty
// allowedFlexVolumes (every installed driver, run as root on the node), and direct network volumes
// (nfs, iscsi, rbd, cephFS) that bypass PersistentVolume RBAC. $privVolumes is only * and hostPath.
func TestStress_SCC_HostReachingVolumesUngated(t *testing.T) {
	t.Parallel()
	vols := map[string]string{"x-vol-flex": "flexVolume", "x-vol-nfs": "nfs", "x-vol-iscsi": "iscsi", "x-vol-rbd": "rbd", "x-vol-ceph": "cephFS"}
	objs := stressSCCs(t, "", func(sccs map[string]interface{}) {
		for n, v := range vols {
			sccs[n] = sccEntry(map[string]interface{}{"volumes": []interface{}{"secret", v}})
		}
	})
	assertGated(t, objs, vols)
}

// ---------------------------------------------------------------------------------------------
// Invalid objects the hub accepts: the entry reaches the API server and fails there, instead of
// being reported per entry in custom-sccs-status.
// ---------------------------------------------------------------------------------------------

func TestStress_SCC_InvalidSpecReachesAPIServer(t *testing.T) {
	t.Parallel()
	cases := map[string]map[string]interface{}{
		// priority is int32 in the SCC schema.
		"x-prio-overflow": {"priority": 2147483648},
		// SCC validation: defaultAllowPrivilegeEscalation true needs allowPrivilegeEscalation true.
		"x-dape": {"defaultAllowPrivilegeEscalation": true, "allowPrivilegeEscalation": false},
		// uid is int64; a string never decodes.
		"x-uid-string": {"runAsUser": map[string]interface{}{"type": "MustRunAs", "uid": " 1000"}},
		// Not FSType values (FSType is case-sensitive): rejected by the API server or a silent no-op.
		"x-vol-case": {"volumes": []interface{}{"HostPath", "Secret"}},
		// seLinuxOptions must be a map.
		"x-seopts-string": {"seLinuxContext": map[string]interface{}{"type": "MustRunAs", "seLinuxOptions": "spc_t"}},
	}
	objs := stressSCCs(t, "", func(sccs map[string]interface{}) {
		for n, s := range cases {
			sccs[n] = sccEntry(s)
		}
	})
	data := sccStatusData(t, objs)
	for n, s := range cases {
		if sccObject(objs, sccEnforceCP, "SecurityContextConstraints", "", n) != nil {
			b, _ := json.Marshal(s)
			t.Errorf("%s: spec %s is invalid for the SCC API but was rendered (status %q)", n, b, data[n])
		}
	}
}

// Status keys must be valid ConfigMap keys, or the one status ConfigMap that carries every error
// is itself rejected and every entry's error is lost.
func TestStress_SCC_StatusKeyNotAValidConfigMapKey(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("a", 300)
	objs := stressSCCs(t, "", func(sccs map[string]interface{}) {
		sccs[""] = map[string]interface{}{}
		sccs["."] = map[string]interface{}{}
		sccs[".."] = map[string]interface{}{}
		sccs["..x"] = map[string]interface{}{}
		sccs[long] = map[string]interface{}{}
	})
	data := sccStatusData(t, objs)
	cmKey := regexp.MustCompile(`^[-._a-zA-Z0-9]+$`)
	for k := range data {
		if len(k) > 253 || !cmKey.MatchString(k) || k == "." || strings.HasPrefix(k, "..") {
			t.Errorf("status key %q (len %d) is not a valid ConfigMap key: the API server rejects custom-sccs-status", k, len(k))
		}
	}
}

// ---------------------------------------------------------------------------------------------
// Foreign RBAC objects of the generated names are taken over (the SCC itself is protected).
// ---------------------------------------------------------------------------------------------

const foreignSCCRBAC = `apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: autoshift-scc-example-fixed-uid
rules:
  - apiGroups: [""]
    resources: [configmaps]
    verbs: [get]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: autoshift-scc-example-fixed-uid
  namespace: example-ci
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: view
subjects:
  - kind: User
    name: alice
`

// A ClusterRole autoshift-scc-<name>, or a RoleBinding of that name in a listed namespace (which
// any namespace admin can create), that custom-sccs does not own is overwritten with mustonlyhave:
// the ClusterRole's rules are replaced, and the RoleBinding's immutable roleRef makes the update fail
// on every evaluation. The SCC gets an ownership check; these do not.
func TestStress_SCC_ForeignRBACTakenOver(t *testing.T) {
	t.Parallel()
	objs := stressSCCs(t, foreignSCCRBAC, nil)
	if sccObject(objs, sccEnforceCP, "ClusterRole", "", "autoshift-scc-example-fixed-uid") != nil {
		t.Error("foreign ClusterRole autoshift-scc-example-fixed-uid (no managed-by label) is overwritten")
	}
	if sccObject(objs, sccEnforceCP, "RoleBinding", "example-ci", "autoshift-scc-example-fixed-uid") != nil {
		t.Error("foreign RoleBinding example-ci/autoshift-scc-example-fixed-uid (no managed-by label) is overwritten")
	}
	if got := sccStatusData(t, objs)["example-fixed-uid"]; got == "" {
		t.Error("foreign RBAC object not reported in custom-sccs-status")
	}
}

// ---------------------------------------------------------------------------------------------
// What held up. These pass; they pin the behavior the attacks above did not break.
// ---------------------------------------------------------------------------------------------

// Privileged capability spellings: case, CAP_ prefix, and Unicode that upper-cases to ASCII
// (U+017F long s, U+0131 dotless i), which CRI-O also upper-cases.
func TestStress_SCC_CapabilitySpellingsGated(t *testing.T) {
	t.Parallel()
	spellings := map[string]string{
		"x-c1": "sys_admin", "x-c2": "Cap_Sys_Admin", "x-c3": "CAP_NET_RAW", "x-c4": "cap_all",
		"x-c5": "ſys_admin", "x-c6": "net_raw", "x-c7": "Bpf", "x-c8": "ſyslog",
		"x-c9": "dac_read_ſearch", "x-c10": "ALL", "x-c11": "*",
	}
	objs := stressSCCs(t, "", func(sccs map[string]interface{}) {
		for n, c := range spellings {
			sccs[n] = sccEntry(map[string]interface{}{"defaultAddCapabilities": []interface{}{c}})
		}
	})
	assertGated(t, objs, spellings)
}

func TestStress_SCC_RootUIDSpellingsGated(t *testing.T) {
	t.Parallel()
	uids := map[string]interface{}{"x-u1": "0", "x-u2": "00", "x-u3": 0.0, "x-u4": "-0", "x-u5": "0.0", "x-u6": -5, "x-u7": "-1"}
	names := map[string]string{}
	objs := stressSCCs(t, "", func(sccs map[string]interface{}) {
		for n, u := range uids {
			names[n] = "uid"
			sccs[n] = sccEntry(map[string]interface{}{"runAsUser": map[string]interface{}{"type": "MustRunAs", "uid": u}})
			names[n+"r"] = "uidRangeMin"
			sccs[n+"r"] = sccEntry(map[string]interface{}{"runAsUser": map[string]interface{}{"type": "MustRunAsRange", "uidRangeMin": u, "uidRangeMax": 5000}})
		}
	})
	assertGated(t, objs, names)
}

func TestStress_SCC_SeccompSELinuxVolumeSpellingsGated(t *testing.T) {
	t.Parallel()
	cases := map[string]map[string]interface{}{
		"x-s1": {"seccompProfiles": []interface{}{"UNCONFINED"}},
		"x-s2": {"seccompProfiles": []interface{}{"runtime/default", "Unconfined"}},
		"x-s3": {"seccompProfiles": []interface{}{"*"}},
		"x-l1": {"seLinuxContext": map[string]interface{}{"type": "MustRunAs", "seLinuxOptions": map[string]interface{}{"type": "SPC_T"}}},
		"x-l2": {"seLinuxContext": map[string]interface{}{"type": "MustRunAs", "seLinuxOptions": map[string]interface{}{"type": "Unconfined_T"}}},
		"x-v1": {"volumes": []interface{}{"*"}},
		"x-v2": {"volumes": []interface{}{"hostPath"}},
		"x-h1": {"allowHostDirVolumePlugin": true},
		"x-k1": {"allowedUnsafeSysctls": []interface{}{"kernel.*"}},
		"x-k2": {"allowedUnsafeSysctls": []interface{}{"*"}},
	}
	names := map[string]string{}
	objs := stressSCCs(t, "", func(sccs map[string]interface{}) {
		for n, s := range cases {
			names[n] = n
			sccs[n] = sccEntry(s)
		}
	})
	assertGated(t, objs, names)
}

// Field-name casing never smuggles a field past the gate: unknown names are errors.
func TestStress_SCC_FieldCasingRejected(t *testing.T) {
	t.Parallel()
	cases := map[string]map[string]interface{}{
		"x-f1": {"AllowPrivilegedContainer": true},
		"x-f2": {"allowprivilegedcontainer": true},
		"x-f3": {"allowPrivilegedContainer ": true},
		"x-f4": {"Users": []interface{}{"system:admin"}},
		"x-f5": {"runAsUser": map[string]interface{}{"Type": "RunAsAny"}},
		"x-f6": {"runAsUser": map[string]interface{}{"type": "runasany"}},
		"x-f7": {"allowPrivilegedContainer": "true"},
	}
	names := map[string]string{}
	objs := stressSCCs(t, "", func(sccs map[string]interface{}) {
		for n, s := range cases {
			names[n] = n
			sccs[n] = sccEntry(s)
		}
		sccs["x-f8"] = map[string]interface{}{"allowprivileged": true, "spec": map[string]interface{}{"allowHostPID": true}}
		names["x-f8"] = "entry key casing"
	})
	assertGated(t, objs, names)
}

// Built-in SCC names in any spelling are rejected (by the DNS-1123 check or the reserved list).
func TestStress_SCC_BuiltinNameSpellingsRejected(t *testing.T) {
	t.Parallel()
	names := map[string]string{}
	objs := stressSCCs(t, "", func(sccs map[string]interface{}) {
		for _, n := range []string{"Restricted-v2", "anyuid ", "privileged.", " privileged", "PRIVILEGED", "nonroot-v2", "hostmount-anyuid-v2", "restricted-v3"} {
			names[n] = "built-in"
			sccs[n] = map[string]interface{}{}
		}
	})
	data := sccStatusData(t, objs)
	for n := range names {
		if sccObject(objs, sccEnforceCP, "SecurityContextConstraints", "", n) != nil {
			t.Errorf("%q rendered", n)
		}
		key := regexp.MustCompile(`[^-._a-zA-Z0-9]`).ReplaceAllString(n, "_")
		if data[key] == "" {
			t.Errorf("%q not reported (status %v)", n, data)
		}
	}
}

// serviceAccounts shapes that would widen the subjects are rejected, and subjects are exactly the
// true ServiceAccounts.
func TestStress_SCC_ServiceAccountShapesRejected(t *testing.T) {
	t.Parallel()
	refs := []string{"*", "system:serviceaccounts", "*/builder", "example-ci/*", "example-ci/", "/builder",
		"example-ci/a:b", "openshift-*/x", "example-ci/system:serviceaccounts", "Example-ci/builder",
		"example-ci/builder/x", "example-ci /builder", "example-ci/builder "}
	objs := stressSCCs(t, "", func(sccs map[string]interface{}) {
		for i, ref := range refs {
			sccs["x-sa"+strings.Repeat("a", i+1)] = map[string]interface{}{"serviceAccounts": map[string]interface{}{ref: true}}
		}
	})
	data := sccStatusData(t, objs)
	for i, ref := range refs {
		n := "x-sa" + strings.Repeat("a", i+1)
		if !strings.Contains(data[n], "is not <namespace>/<serviceaccount>") {
			t.Errorf("serviceAccounts key %q accepted (status %q)", ref, data[n])
		}
		if sccObject(objs, sccEnforceCP, "SecurityContextConstraints", "", n) != nil {
			t.Errorf("serviceAccounts key %q: entry rendered", ref)
		}
	}
	for _, o := range objs {
		if o.obj["kind"] == "SecurityContextConstraints" && o.complianceType != "mustnothave" {
			if len(strList(o.obj["users"])) != 0 || len(strList(o.obj["groups"])) != 0 {
				t.Errorf("SCC %v has users/groups", o.obj["metadata"])
			}
		}
		if o.obj["kind"] == "RoleBinding" && o.complianceType != "mustnothave" {
			for _, s := range o.obj["subjects"].([]interface{}) {
				if s.(map[string]interface{})["kind"] != "ServiceAccount" {
					t.Errorf("RoleBinding subject %v is not a ServiceAccount", s)
				}
			}
		}
	}
}

// Template delimiters and quoting in every string field stay literal (status messages included).
func TestStress_SCC_InjectionEverywhereLiteral(t *testing.T) {
	t.Parallel()
	evil := `"}} {{ fail "pwn" }} {{hub fail "pwn" hub}}\` + "\n"
	objs := stressSCCs(t, "", func(sccs map[string]interface{}) {
		sccs["example-inj"] = map[string]interface{}{"spec": map[string]interface{}{
			"forbiddenSysctls":         []interface{}{evil},
			"requiredDropCapabilities": []interface{}{evil},
			"seLinuxContext":           map[string]interface{}{"type": "MustRunAs", "seLinuxOptions": map[string]interface{}{"user": evil, "level": evil}},
			"fsGroup":                  map[string]interface{}{"type": "MustRunAs", "ranges": []interface{}{map[string]interface{}{"min": 1, "max": evil}}},
			"allowedFlexVolumes":       []interface{}{map[string]interface{}{"driver": evil}},
		}}
		sccs["x-inj-bad"] = map[string]interface{}{"serviceAccounts": map[string]interface{}{evil: evil}, "spec": map[string]interface{}{"allowedCapabilities": []interface{}{"SYS_ADMIN"}}}
		sccs[evil] = map[string]interface{}{}
	})
	o := sccObject(objs, sccEnforceCP, "SecurityContextConstraints", "", "example-inj")
	if o == nil {
		t.Fatalf("example-inj not rendered; status %v", sccStatusData(t, objs))
	}
	if got := strList(o.obj["forbiddenSysctls"]); len(got) != 1 || got[0] != evil {
		t.Errorf("forbiddenSysctls = %q, want literal", got)
	}
	data := sccStatusData(t, objs)
	if !strings.Contains(data["x-inj-bad"], "is not true or false") || !strings.Contains(data["x-inj-bad"], "SYS_ADMIN") {
		t.Errorf("x-inj-bad status = %q", data["x-inj-bad"])
	}
}

// Forged managed-by label on a pre-existing SCC: by the README's ownership rule the label is the
// ownership claim, so the SCC is managed (spec replaced). Pinned so a change is deliberate.
func TestStress_SCC_ForgedLabelIsOwnership(t *testing.T) {
	t.Parallel()
	forged := `apiVersion: security.openshift.io/v1
kind: SecurityContextConstraints
metadata:
  name: example-forged
  labels:
    autoshift.io/managed-by: custom-sccs
allowPrivilegedContainer: true
users: [system:admin]
groups: [system:authenticated]
`
	objs := stressSCCs(t, forged, func(sccs map[string]interface{}) {
		sccs["example-forged"] = map[string]interface{}{}
	})
	o := sccObject(objs, sccEnforceCP, "SecurityContextConstraints", "", "example-forged")
	if o == nil {
		t.Fatal("labelled SCC not managed")
	}
	if o.complianceType != "mustonlyhave" || o.obj["allowPrivilegedContainer"] != false || len(strList(o.obj["groups"])) != 0 {
		t.Errorf("forged SCC not reset to the declared posture: %v", o.obj)
	}
}

// Disabled, removed and renamed entries: reported by inform only, never mustnothave in enforce,
// under both remediation settings of the inform ConfigurationPolicy.
func TestStress_SCC_RemovalNeverDeletes(t *testing.T) {
	t.Parallel()
	variants := map[string]func(sccs map[string]interface{}){
		"disabled": func(s map[string]interface{}) { s["example-fixed-uid"].(map[string]interface{})["enabled"] = false },
		"removed":  func(s map[string]interface{}) { delete(s, "example-fixed-uid") },
		"renamed": func(s map[string]interface{}) {
			s["example-fixed-uid-2"] = s["example-fixed-uid"]
			delete(s, "example-fixed-uid")
		},
		"all gone": func(s map[string]interface{}) {
			for k := range s {
				delete(s, k)
			}
		},
	}
	for name, mutate := range variants {
		t.Run(name, func(t *testing.T) {
			objs := stressSCCs(t, "", mutate)
			for _, o := range objs {
				if o.complianceType == "mustnothave" && o.remediationAction != "inform" {
					meta := o.obj["metadata"].(map[string]interface{})
					if meta["name"] != sccStatus {
						t.Errorf("%s ConfigurationPolicy would delete %v %v", o.remediationAction, o.obj["kind"], meta["name"])
					}
				}
			}
			if got := sccStale(objs, sccInformCP); !got["RoleBinding|example-old|autoshift-scc-example-fixed-uid"] {
				t.Errorf("stale RoleBinding not reported: %v", got)
			}
		})
	}
}

// The stale reports live in their own Policy, policy-custom-sccs-test, whose root is inform: bundled
// into the enforcing Policy, a root remediationAction would override the always-inform child and
// turn every stale mustnothave into a delete.
func TestStress_SCC_InformChecksNotBundled(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	for _, remediation := range []string{"enforce", "inform"} {
		rendered, err := cachedKustomizeBuild(filepath.Join(root, "policies", customSCCsPolicy), remediation)
		if err != nil {
			t.Fatal(err)
		}
		assertEnforceTestSplit(t, rendered, remediation, splitPolicyDirs[customSCCsPolicy])
	}
}

// The Secret-lookup payload in every custom-sccs string field: refused or literal. stressSCCs fails
// the test when anything is left for the managed cluster that it cannot resolve; here the payload
// must also never be replaced by the pull-secret data.
func TestStress_SCC_SecretLookupLiteral(t *testing.T) {
	t.Parallel()
	const lookupPayload = "{{ (lookup `v1` `Secret` `openshift-config` `pull-secret`).data }}"
	specs := map[string]map[string]interface{}{
		"forbiddenSysctls":         {"forbiddenSysctls": []interface{}{lookupPayload}},
		"allowedUnsafeSysctls":     {"allowedUnsafeSysctls": []interface{}{lookupPayload}},
		"requiredDropCapabilities": {"requiredDropCapabilities": []interface{}{lookupPayload}},
		"allowedCapabilities":      {"allowedCapabilities": []interface{}{lookupPayload}},
		"defaultAddCapabilities":   {"defaultAddCapabilities": []interface{}{lookupPayload}},
		"seccompProfiles":          {"seccompProfiles": []interface{}{lookupPayload}},
		"volumes":                  {"volumes": []interface{}{lookupPayload}},
		"allowedFlexVolumes":       {"allowedFlexVolumes": []interface{}{map[string]interface{}{"driver": lookupPayload}}},
		"seLinux user":             {"seLinuxContext": map[string]interface{}{"type": "MustRunAs", "seLinuxOptions": map[string]interface{}{"user": lookupPayload}}},
		"seLinux role":             {"seLinuxContext": map[string]interface{}{"type": "MustRunAs", "seLinuxOptions": map[string]interface{}{"role": lookupPayload}}},
		"seLinux type":             {"seLinuxContext": map[string]interface{}{"type": "MustRunAs", "seLinuxOptions": map[string]interface{}{"type": lookupPayload}}},
		"seLinux level":            {"seLinuxContext": map[string]interface{}{"type": "MustRunAs", "seLinuxOptions": map[string]interface{}{"level": lookupPayload}}},
		"seLinuxContext type":      {"seLinuxContext": map[string]interface{}{"type": lookupPayload}},
		"runAsUser type":           {"runAsUser": map[string]interface{}{"type": lookupPayload}},
		"runAsUser uid":            {"runAsUser": map[string]interface{}{"type": "MustRunAs", "uid": lookupPayload}},
		"fsGroup type":             {"fsGroup": map[string]interface{}{"type": lookupPayload}},
		"fsGroup range":            {"fsGroup": map[string]interface{}{"type": "MustRunAs", "ranges": []interface{}{map[string]interface{}{"min": 1, "max": lookupPayload}}}},
		"supplementalGroups type":  {"supplementalGroups": map[string]interface{}{"type": lookupPayload}},
		"priority":                 {"priority": lookupPayload},
		"readOnlyRootFilesystem":   {"readOnlyRootFilesystem": lookupPayload},
		"allowPrivileged":          {"allowPrivileged": lookupPayload},
		"unknown field":            {"x-extra": lookupPayload},
	}
	check := func(field string, objs []onboardingObject) {
		t.Helper()
		for _, o := range objs {
			if b, _ := json.Marshal(o.obj); strings.Contains(string(b), "dockerconfigjson") {
				t.Errorf("%s: pull-secret data rendered into %v %v", field, o.obj["kind"], o.obj["metadata"])
			}
		}
	}
	for field, spec := range specs {
		check(field, stressSCCs(t, "", func(sccs map[string]interface{}) { sccs["example-lookup"] = sccEntry(spec) }))
	}
	check("serviceAccounts key", stressSCCs(t, "", func(sccs map[string]interface{}) {
		sccs["example-lookup"] = map[string]interface{}{"serviceAccounts": map[string]interface{}{lookupPayload: true}}
	}))
	check("serviceAccounts value", stressSCCs(t, "", func(sccs map[string]interface{}) {
		sccs["example-lookup"] = map[string]interface{}{"serviceAccounts": map[string]interface{}{"example-ci/builder": lookupPayload}}
	}))
	check("enabled", stressSCCs(t, "", func(sccs map[string]interface{}) {
		sccs["example-lookup"] = map[string]interface{}{"enabled": lookupPayload}
	}))
	check("entry name", stressSCCs(t, "", func(sccs map[string]interface{}) { sccs[lookupPayload] = sccEntry(map[string]interface{}{}) }))
	objs := stressSCCs(t, "", func(sccs map[string]interface{}) {
		sccs["example-lookup"] = sccEntry(map[string]interface{}{"forbiddenSysctls": []interface{}{lookupPayload}})
	})
	o := sccObject(objs, sccEnforceCP, "SecurityContextConstraints", "", "example-lookup")
	if o == nil {
		return // refused
	}
	if got := strList(o.obj["forbiddenSysctls"]); len(got) != 1 || got[0] != lookupPayload {
		t.Errorf("forbiddenSysctls = %q, want literal", got)
	}
}

// ServiceAccounts in system namespaces (kube-system, kube-public, default, openshift, openshift-*)
// are granted use exactly like any other: no gate, and the README says nothing about them.
// default/default is the ServiceAccount of every pod in default that names none, so the grant
// reaches all of them. The non-privileged entry here (allowPrivilegeEscalation, SETUID, SETGID) is
// already looser than restricted-v2; the privileged entry's allowPrivileged acknowledges the
// posture, not who receives it.
func TestStress_SCC_SystemNamespaceGrantUngated(t *testing.T) {
	t.Parallel()
	sysNs := []string{"kube-system", "kube-public", "default", "openshift", "openshift-monitoring", "openshift-config"}
	var stub strings.Builder
	for _, ns := range sysNs {
		stub.WriteString("apiVersion: v1\nkind: Namespace\nmetadata:\n  name: " + ns + "\n---\n")
	}
	sas := map[string]interface{}{}
	for _, ns := range sysNs {
		sas[ns+"/default"] = true
	}
	objs := stressSCCs(t, stub.String(), func(sccs map[string]interface{}) {
		sccs["example-loose"] = map[string]interface{}{"serviceAccounts": sas, "spec": map[string]interface{}{
			"allowPrivilegeEscalation": true, "allowedCapabilities": []interface{}{"SETUID", "SETGID"}}}
		sccs["example-priv"] = map[string]interface{}{"serviceAccounts": sas, "allowPrivileged": true, "spec": map[string]interface{}{
			"allowedCapabilities": []interface{}{"SYS_ADMIN"}}}
	})
	data := sccStatusData(t, objs)
	for _, entry := range []string{"example-loose", "example-priv"} {
		for _, ns := range sysNs {
			if rb := sccObject(objs, sccEnforceCP, "RoleBinding", ns, "autoshift-scc-"+entry); rb != nil {
				t.Errorf("%s: use granted to %s/default with no gate (status %q)", entry, ns, data[entry])
			}
		}
	}
}

// assertRootRemediationUnset fails when the rendered root Policy sets remediationAction, which
// would override an always-inform child and turn its mustnothave reports into deletes.
func assertRootRemediationUnset(t *testing.T, rendered string) {
	t.Helper()
	for _, doc := range splitYAMLDocuments(rendered) {
		var obj map[string]interface{}
		if err := sigsyaml.Unmarshal([]byte(doc), &obj); err != nil || obj["kind"] != "Policy" {
			continue
		}
		if ra, ok := obj["spec"].(map[string]interface{})["remediationAction"]; ok {
			t.Errorf("root Policy %v sets remediationAction %v", obj["metadata"].(map[string]interface{})["name"], ra)
		}
	}
}
