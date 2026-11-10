package configkeys

import (
	"reflect"
	"testing"
)

func TestBuildReport_DirectoryNameForms(t *testing.T) {
	dirs := []string{"openshift-upgrade", "workload-partitioning", "quay"}
	conv := &Conventions{Shared: []string{"aws"}, Aliases: map[string]string{}}

	cases := []struct {
		name          string
		declared      []string
		wantUnmapped  []string
		wantAmbiguous []string
	}{
		{name: "camel case", declared: []string{"openshiftUpgrade"}},
		{name: "verbatim", declared: []string{"openshift-upgrade"}},
		{name: "single word", declared: []string{"quay"}},
		{name: "shared", declared: []string{"aws"}},
		{name: "unknown", declared: []string{"openshiftUpgrades"}, wantUnmapped: []string{"openshiftUpgrades"}},
		{
			name:          "both forms",
			declared:      []string{"openshift-upgrade", "openshiftUpgrade"},
			wantAmbiguous: []string{"openshift-upgrade and openshiftUpgrade"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			declared := map[string][]string{}
			for _, k := range tc.declared {
				declared[k] = []string{"_example.yaml"}
			}
			rep := BuildReport(declared, dirs, conv)
			if !reflect.DeepEqual(rep.Unmapped, tc.wantUnmapped) {
				t.Errorf("Unmapped = %v, want %v", rep.Unmapped, tc.wantUnmapped)
			}
			if !reflect.DeepEqual(rep.Ambiguous, tc.wantAmbiguous) {
				t.Errorf("Ambiguous = %v, want %v", rep.Ambiguous, tc.wantAmbiguous)
			}
		})
	}
}
