//go:build integration

package resolver

import (
	"fmt"
	"strings"
	"testing"
)

// A platform key is refused unless projectDefaults.allowedPlatformKeys maps that exact key to true;
// another key under the same prefix stays refused, and a non-reserved key such as
// k8s.ovn.org/primary-user-defined-network is never affected.
func TestProjectOnboarding_AllowedPlatformKeys(t *testing.T) {
	t.Parallel()
	r := stressRun(t, stressOpts{mutate: func(cfg map[string]interface{}) {
		cfg["projectDefaults"].(map[string]interface{})["allowedPlatformKeys"] = map[string]interface{}{
			"pod-security.kubernetes.io/warn": true,
			"openshift.io/node-selector":      false,
		}
		stressProject(cfg, "reporting")["labels"] = map[string]interface{}{
			"psa-warn":    kv("pod-security.kubernetes.io/warn", "restricted"),
			"psa-enforce": kv("pod-security.kubernetes.io/enforce", "privileged"),
			"udn":         kv("k8s.ovn.org/primary-user-defined-network", "x"),
		}
		stressProject(cfg, "reporting")["annotations"] = map[string]interface{}{
			"node-selector": kv("openshift.io/node-selector", "example.com/pool=a"),
		}
	}})
	ns := namespaceNamed(r.objs, "reporting")
	if ns == nil {
		t.Fatalf("reporting not rendered, status %v", stressStatus(t, r.objs))
	}
	lbls, ann := stringMap(ns, "labels"), stringMap(ns, "annotations")
	if lbls["pod-security.kubernetes.io/warn"] != "restricted" {
		t.Errorf("allowed key pod-security.kubernetes.io/warn not applied, labels %v", lbls)
	}
	if _, ok := lbls["pod-security.kubernetes.io/enforce"]; ok {
		t.Errorf("pod-security.kubernetes.io/enforce is not listed but applied")
	}
	if lbls["k8s.ovn.org/primary-user-defined-network"] != "x" {
		t.Errorf("k8s.ovn.org/primary-user-defined-network is not reserved but was not applied, labels %v", lbls)
	}
	if _, ok := ann["openshift.io/node-selector"]; ok {
		t.Errorf("openshift.io/node-selector maps to false but is applied")
	}
	st := stressStatus(t, r.objs)["reporting"]
	for _, want := range []string{
		"label pod-security.kubernetes.io/enforce is reserved for the platform",
		"annotation openshift.io/node-selector is reserved for the platform",
	} {
		if !strings.Contains(st, want) {
			t.Errorf("status %q, want it to contain %q", st, want)
		}
	}
	if strings.Contains(st, "pod-security.kubernetes.io/warn") || strings.Contains(st, "primary-user-defined-network") {
		t.Errorf("status %q reports an allowed or non-reserved key", st)
	}
}

// Platform namespaces are refused with no override; names that only resemble them are not, and
// neither is any project of the deployment-shaped config.
func TestProjectOnboarding_PlatformNamespaces(t *testing.T) {
	t.Parallel()
	refused := []string{"kube-system", "kube-public", "kube-x", "openshift", "openshift-dns", "default",
		"open-cluster-management", "open-cluster-management-agent-addon", "multicluster-engine", "hive"}
	allowed := []string{"kubernetes-x", "openshiftx", "my-openshift-app", "defaults", "hive-x", "x-default"}
	for name := range stressDeploymentConfig(t)["projects"].(map[string]interface{}) {
		allowed = append(allowed, name)
	}
	r := stressRun(t, stressOpts{mutate: func(cfg map[string]interface{}) {
		projects := stressProjects(cfg)
		for i, ns := range append(append([]string{}, refused...), allowed...) {
			projects[fmt.Sprintf("p%02d", i)] = map[string]interface{}{"namespace": ns, "groups": map[string]interface{}{"admin": "X"}}
		}
	}})
	st := stressStatus(t, r.objs)
	for i, ns := range refused {
		want := "namespace " + ns + " is a platform namespace, which project onboarding never manages"
		if got := st[fmt.Sprintf("p%02d", i)]; got != want {
			t.Errorf("namespace %s: status %q, want %q", ns, got, want)
		}
		if namespaceNamed(r.objs, ns) != nil {
			t.Errorf("platform namespace %s rendered", ns)
		}
	}
	for i, ns := range allowed {
		if got := st[fmt.Sprintf("p%02d", len(refused)+i)]; strings.Contains(got, "platform namespace") {
			t.Errorf("namespace %s refused as a platform namespace: %q", ns, got)
		}
	}
}

// An ingress allow from cidrs renders an ipBlock that excepts every cluster and service network
// lying inside it, since OVN-Kubernetes matches pod IPs with ipBlock. A CIDR inside one of them is
// an error, and so is any ingress cidrs allow while those networks are unknown.
func TestProjectOnboarding_IngressCidrsExceptClusterNetworks(t *testing.T) {
	t.Parallel()
	cases := map[string][]string{
		"0.0.0.0/0":       {"10.128.0.0/14", "172.30.0.0/16"},
		"10.0.0.0/8":      {"10.128.0.0/14"},
		"198.51.100.0/24": nil,
		"2001:db8::/32":   nil,
	}
	for c, want := range cases {
		t.Run(c, func(t *testing.T) {
			r := stressRun(t, stressOpts{mutate: func(cfg map[string]interface{}) {
				withRule("strict", "ext", ingressFrom("allow", cidrs(c), 443))(stressProjects(cfg))
			}})
			np := networkPolicyNamed(r.objs, "reporting", "autoshift-rule-ext")
			if np == nil {
				t.Fatalf("no NetworkPolicy, status %q", stressStatus(t, r.objs)["reporting"])
			}
			block := map[string]interface{}{"cidr": c}
			if want != nil {
				except := []interface{}{}
				for _, w := range want {
					except = append(except, w)
				}
				block["except"] = except
			}
			wantRules := asJSON([]interface{}{map[string]interface{}{
				"from":  []interface{}{map[string]interface{}{"ipBlock": block}},
				"ports": []interface{}{map[string]interface{}{"port": 443, "protocol": "TCP"}},
			}})
			if got := asJSON(npRules(np, "ingress")); got != wantRules {
				t.Errorf("ingress rules\n got %s\nwant %s", got, wantRules)
			}
		})
	}
	t.Run("networks unknown", func(t *testing.T) {
		res := runOnboardingAll(t, testdataWithout(t, onboardingNetworkStubs, ""), nil, func(c *ExampleConfigs) {
			c.ClusterInstallConfig["networking"] = map[string]interface{}{"clusterNetwork": []interface{}{}, "serviceNetwork": []interface{}{}}
			withRule("strict", "ext", ingressFrom("allow", cidrs("198.51.100.0/24"), 443))(stressProjects(c.HubConfig))
		})
		objs := onboardingObjects(t, res)
		if networkPolicyNamed(objs, "reporting", "autoshift-rule-ext") != nil {
			t.Error("ingress cidrs allow rendered while the cluster networks are unknown")
		}
		if st := onboardingStatus(t, objs)["reporting"]; !strings.Contains(st, "cannot check ingress cidr 198.51.100.0/24") {
			t.Errorf("status %q, want the unchecked ingress cidr reported", st)
		}
	})
}
