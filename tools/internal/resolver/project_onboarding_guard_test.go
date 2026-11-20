//go:build integration

package resolver

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Guard against template injection into the managed-cluster template. The hub resolves
// {{hub ... hub}} first; everything it emits becomes text that config-policy-controller parses as a
// template on the managed cluster. A user string emitted as template text (even through toJson) is
// parsed there: a group name holding {{ ... }} breaks or executes code for every project. So every
// hub expression that emits text must either pass its value as quoted JSON data, which the managed
// cluster reads back with base64dec and fromJson inside an action:
//
//	{{- $x := {{hub $value | toJson | base64enc | quote hub}} | base64dec | fromJson }}
//
// Base64 rather than quoted JSON: go-template-utils rewrites the template text with regexes before
// parsing it (processForDataTypes for toInt, toBool, toLiteral; processForAutoIndent), and those
// match inside a quoted string, so a value such as `x: {{ y | toInt` would still break the template.
// Base64 output holds none of the characters those patterns need.
//
// or be on the list below: constants, numbers, booleans, and values the hub validated against a
// strict pattern (DNS-1123 names, label syntax, CIDR and IP syntax, quantities, port numbers) that
// cannot hold a template delimiter. Adding an emission of a free-form string fails this test.
var onboardingSafeEmissions = map[string]string{
	`$a.direction`:                         "validated: ingress or egress",
	`$a.subject | toJson`:                  "admin subject: validated label keys, values and operators",
	`$adopt`:                               "bool",
	`$cidr | toJson`:                       "constant pattern",
	`$dir`:                                 "constant list",
	`$e | toJson`:                          "EgressFirewall and AdminNetworkPolicy entries: validated CIDRs, DNS names, ports, names and selectors",
	`$eipIPs | toJson`:                     "validated IP addresses",
	`$field | toJson`:                      "limitRange field from a fixed list",
	`$k | toJson`:                          "quota and limitRange keys validated as resource names",
	`$level`:                               "constant list",
	`$name | toJson`:                       "project name, DNS-1123 label (emitted only for a valid project)",
	`$namespace | toJson`:                  "namespace, DNS-1123 label (emitted only for a valid project)",
	`$peer | toJson`:                       "NetworkPolicy peers: validated selectors and namespace names",
	`$port | toJson`:                       "validated port number or name and protocol",
	`$platformNs | toJson`:                 "constant pattern",
	`$pruneOn`:                             "bool",
	`$shard`:                               "number 0 to 7, from the Policy name the hub validated against ^policy-project-onboarding-[0-7]$",
	`$shards`:                              "constant 8",
	`$sizeAll`:                             "number, in a comment",
	`$sizeLimit`:                           "number, in a comment",
	`$r.direction`:                         "validated: ingress or egress",
	`$statusKey | toJson`:                  "sanitised to [-._a-zA-Z0-9]",
	`$strip`:                               "bool",
	`$want`:                                "constant",
	`add 10 $i`:                            "number",
	`len $efEntries`:                       "number",
	`printf "%v" $v | toJson`:              "quantities validated against the quantity pattern",
	`printf "autoshift-%s" $name | toJson`: "DNS-1123 project name",
	`printf "autoshift-admin-%s" $a.name | toJson`:           "DNS-1123 admin entry name",
	`printf "autoshift-project-%s" $name | toJson`:           "DNS-1123 project name",
	`printf "autoshift-rule-%s" $r.name | toJson`:            "DNS-1123 rule name",
	`ternary "Ingress" "Egress" (eq $r.direction "ingress")`: "constant",
	`ternary "from" "to" (eq $r.direction "ingress")`:        "constant",
	`join "," $otherNs | toJson`:                             "namespaces validated as DNS-1123 labels, joined by commas",
	`join "," $allNames | toJson`:                            "project names validated as label values, joined by commas",
	`$maxDel`:                                                "number, validated against ^[0-9]{1,9}$ or an integral float64 from 0 to 999999999",
	`$graceSec`:                                              "number of seconds, summed on the hub from the digits of a ^([0-9]{1,5}h)?([0-9]{1,7}m)?([0-9]{1,9}s)?$ value",
}

// onboardingEmissionSites pins every allowed direct emission to its position: the expression, then
// " @ ", then the last 24 characters before it on its line (trimmed), with the number of such sites
// in each projects.yaml. An approved expression used in a new place, for example $k | toJson next to
// an unvalidated value, changes a count or adds a key, and the test fails until the new site is
// reviewed and listed.
var onboardingEmissionSites = map[string]int{
	"$platformNs | toJson @ sName)) (not (regexMatch":            2,
	"$sizeAll @ nboarding size estimate:":                        1,
	"$shard @ oject-onboarding-status-":                          3,
	"$shard @ {{- $shard :=":                                     1,
	"$shards @ adler32sum $objProject))":                         1,
	"$shards @ (adler32sum $nsProject))":                         1,
	`join "," $otherNs | toJson @ ange $n := splitList ","`:      1,
	`join "," $allNames | toJson @ ange $n := splitList ","`:     1,
	`$maxDel @  > %d)" $msg $nsDelCount`:                         1,
	`$maxDel @ {{- if gt $nsDelCount`:                            1,
	`$graceSec @ -02T15:04:05Z" (add $now`:                       1,
	`$graceSec @ 0) (ge (sub $now $since)`:                       1,
	`$graceSec @ 15:04:05Z" (add $c.since`:                       2,
	"$sizeLimit @ zeAll hub}} bytes, limit":                      1,
	`$a.direction @ `:                                            1,
	`$a.subject | toJson @ subject:`:                             1,
	`$adopt @ {{- else if not`:                                   1,
	`$adopt @ {{- if not (and (not`:                              1,
	`$cidr | toJson @ {{- if regexMatch`:                         1,
	`$dir @ `:                                                    1,
	`$e | toJson @ -`:                                            3,
	`$eipIPs | toJson @ egressIPs:`:                              1,
	`$field | toJson @ `:                                         1,
	`$k | toJson @ `:                                             2,
	`$level @ name: 'autoshift-`:                                 1,
	`$name | toJson @ autoshift.io/project:`:                     11,
	`$name | toJson @ {{- if ne $liveOwner`:                      1,
	`$name | toJson @ onboarding-created-by"))`:                  1,
	`$namespace | toJson @  is owned by project %s"`:             1,
	`$namespace | toJson @ name:`:                                1,
	`$namespace | toJson @ namespace:`:                           7,
	`$namespace | toJson @ ng }}{{- $_ := set $live`:             1,
	`$namespace | toJson @ e $k, $v := index $nsAdd`:             1,
	`$namespace | toJson @ okup "v1" "Namespace" ""`:             1,
	`$namespace | toJson @ so it is not taken over"`:             1,
	`$namespace | toJson @ t: true to take it over"`:             1,
	`$namespace | toJson @ {{- $_ := set $rendered`:              1,
	`$peer | toJson @ -`:                                         1,
	`$port | toJson @ -`:                                         1,
	`$pruneOn @ v" $owner.mode) "inform"`:                        1,
	`$pruneOn @ {{- if not`:                                      1,
	`$r.direction @ `:                                            1,
	`$statusKey | toJson @ {{- $_ := set $status`:                8,
	`$statusKey | toJson @ } (append (index $status`:             5,
	`$statusKey | toJson @ } (concat (index $status`:             3,
	`$strip @ t $kept $stripped) $kept`:                          1,
	`$want @  $eligible (eq $target "`:                           1,
	`$want @ {{- if eq $target "`:                                1,
	`add 10 $i @ priority:`:                                      1,
	`len $efEntries @ {{- $efCount :=`:                           1,
	`printf "%v" $v | toJson @ {{hub $k | toJson hub}}:`:         2,
	`printf "autoshift-%s" $name | toJson @ name:`:               1,
	`printf "autoshift-admin-%s" $a.name | toJson @ name:`:       1,
	`printf "autoshift-project-%s" $name | toJson @ name:`:       1,
	`printf "autoshift-rule-%s" $r.name | toJson @ name:`:        1,
	`ternary "Ingress" "Egress" (eq $r.direction "ingress") @ -`: 1,
	`ternary "from" "to" (eq $r.direction "ingress") @ -`:        1,
}

func TestProjectOnboarding_NoUserStringAsSpokeTemplateText(t *testing.T) {
	t.Parallel()
	hubExpr := regexp.MustCompile(`(?s)\{\{hub-?\s(.*?)\s-?hub\}\}`)
	nonEmitting := regexp.MustCompile(`^(\$[\w.]+\s*:?=|if\b|else\b|end\b|range\b|with\b|fail\b|continue\b)`)
	for _, sub := range []string{"manifests", "test"} {
		path := filepath.Join(repoRoot(t), "policies", "stable", "project-onboarding", sub, "projects.yaml")
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		used := map[string]bool{}
		sites := map[string]int{}
		for _, m := range hubExpr.FindAllStringSubmatchIndex(src, -1) {
			expr := strings.TrimSpace(src[m[2]:m[3]])
			if nonEmitting.MatchString(expr) {
				continue
			}
			line := strings.Count(src[:m[0]], "\n") + 1
			if strings.HasSuffix(expr, "| toJson | base64enc | quote") {
				// Data path: the encoded JSON must be decoded inside a managed-cluster action.
				if !strings.HasPrefix(src[m[1]:], " | base64dec | fromJson") {
					t.Errorf("%s:%d: {{hub %s hub}} is encoded but not read back with | base64dec | fromJson", sub, line, expr)
				}
				continue
			}
			if strings.Contains(expr, "| quote") {
				t.Errorf("%s:%d: {{hub %s hub}} passes a quoted string to the managed cluster; its template "+
					"pre-processing matches inside quotes, use | toJson | base64enc | quote and | base64dec | fromJson", sub, line, expr)
				continue
			}
			if _, ok := onboardingSafeEmissions[expr]; !ok {
				t.Errorf("%s:%d: {{hub %s hub}} emits a value into the managed-cluster template as text; "+
					"pass it as data ({{hub ... | toJson | base64enc | quote hub}} | base64dec | fromJson inside an action), or, if the hub "+
					"validates it against a pattern without template delimiters, add it to onboardingSafeEmissions", sub, line, expr)
			}
			used[expr] = true
			lineStart := strings.LastIndex(src[:m[0]], "\n") + 1
			ctx := strings.TrimSpace(src[lineStart:m[0]])
			if len(ctx) > 24 {
				ctx = ctx[len(ctx)-24:]
			}
			sites[expr+" @ "+ctx]++
		}
		for site, n := range sites {
			if want, ok := onboardingEmissionSites[site]; !ok {
				t.Errorf("%s: direct emission at a new position %q (%d sites); review it and add it to onboardingEmissionSites", sub, site, n)
			} else if want != n {
				t.Errorf("%s: direct emission %q has %d sites, onboardingEmissionSites lists %d; review the new or removed site", sub, site, n, want)
			}
		}
		for site := range onboardingEmissionSites {
			if _, ok := sites[site]; !ok {
				t.Errorf("%s: onboardingEmissionSites lists %q, which the template no longer emits; remove it", sub, site)
			}
		}
		for expr := range onboardingSafeEmissions {
			if !used[expr] {
				t.Errorf("%s: onboardingSafeEmissions lists %q, which the template no longer emits; remove it", sub, expr)
			}
		}
	}
}

// Payloads aimed at the managed-cluster template pre-processing (processForDataTypes matches
// `: {{ ... | toInt`, processForAutoIndent matches `... | autoindent }}`), through every kind of
// data path: a group override, a placeholder value, annotations, and an error message that echoes
// the value. Each must render literally (or be reported) and never break the other projects.
func TestProjectOnboarding_PreprocessorPayloads(t *testing.T) {
	t.Parallel()
	for _, payload := range []string{"x: {{ y | toInt", "x | autoindent }}"} {
		t.Run(payload, func(t *testing.T) {
			res := stressRunInput(t, "", nil, func(cfg map[string]interface{}) {
				cfg["projectDefaults"].(map[string]interface{})["annotations"] = map[string]interface{}{
					"owner": map[string]interface{}{"key": "example.com/owner", "value": "{owner}"},
				}
				projects := cfg["projects"].(map[string]interface{})
				proj(projects, "payments-api")["groups"].(map[string]interface{})["read"] = payload
				r := proj(projects, "reporting")
				r["team"] = payload
				r["owner"] = payload
				r["displayName"] = payload
				r["labels"] = map[string]interface{}{"echo": kv(payload, "v")}
			})
			objs := stressObjs(t, res)
			otherProjectsRender(t, objs, "")
			if rb := roleBindingNamed(objs, "payments-api", "autoshift-read"); rb == nil {
				t.Error("payments-api autoshift-read not rendered")
			} else if got := rb["subjects"].([]interface{})[0].(map[string]interface{})["name"]; got != payload {
				t.Errorf("group read rendered %q, want %q", got, payload)
			}
			if rb := roleBindingNamed(objs, "reporting", "autoshift-admin"); rb == nil {
				t.Error("reporting autoshift-admin not rendered")
			} else if got := fmt.Sprint(rb["subjects"].([]interface{})[0].(map[string]interface{})["name"]); !strings.Contains(got, payload) {
				t.Errorf("team placeholder rendered admin group %q, want it to contain %q", got, payload)
			}
			ann := stringMap(namespaceNamed(objs, "reporting"), "annotations")
			for _, k := range []string{"example.com/owner", "openshift.io/display-name"} {
				if ann[k] != payload {
					t.Errorf("annotation %s = %q, want %q", k, ann[k], payload)
				}
			}
			if st := onboardingStatus(t, objs)["reporting"]; !strings.Contains(st, "label "+payload+"=v is not a valid Kubernetes label") {
				t.Errorf("status %q, want the invalid label echoed literally", st)
			}
		})
	}
}
