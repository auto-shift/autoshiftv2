package resolver

import "testing"

func TestStripStringDefaults(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "removes non-empty double-quoted default",
			input: `{{ $platform := (index $ci "platform" | default "baremetal") }}`,
			want:  `{{ $platform := (index $ci "platform") }}`,
		},
		{
			name:  "removes non-empty single-quoted default",
			input: `{{ $arch := (index $ci "cpuArch" | default 'x86_64') }}`,
			want:  `{{ $arch := (index $ci "cpuArch") }}`,
		},
		{
			name:  "keeps empty-string default (crash guard)",
			input: `{{ $v := (index $m "key" | default "") | fromYaml }}`,
			want:  `{{ $v := (index $m "key" | default "") | fromYaml }}`,
		},
		{
			name:  "keeps | default dict",
			input: `{{ $d := (lookup "v1" "CM" "" "") | default dict }}`,
			want:  `{{ $d := (lookup "v1" "CM" "" "") | default dict }}`,
		},
		{
			name:  "keeps | default list",
			input: `{{ $l := (index $ci "addons" | default list) }}`,
			want:  `{{ $l := (index $ci "addons" | default list) }}`,
		},
		{
			name:  "removes multiple defaults in one string",
			input: `{{ $a := (index $m "a" | default "x") }} {{ $b := (index $m "b" | default "y") }}`,
			want:  `{{ $a := (index $m "a") }} {{ $b := (index $m "b") }}`,
		},
		{
			name:  "no-op when no defaults present",
			input: `{{ $x := index $m "key" }} some plain text`,
			want:  `{{ $x := index $m "key" }} some plain text`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := stripStringDefaults(tc.input)
			if got != tc.want {
				t.Errorf("\ninput: %s\n  got: %s\n want: %s", tc.input, got, tc.want)
			}
		})
	}
}
