package labelquery_test

import (
	"reflect"
	"testing"

	"github.com/muandane/gha-scheduler/internal/labelquery"
)

func TestParseWarnsOnUnknownKeys(t *testing.T) {
	var warnings []string
	_, err := labelquery.Parse(
		[]string{"runs-on=1", "gpu=a100", "region=eu-west"},
		labelquery.Defaults{CPU: 2, Arch: "x64"},
		func(key, value string) {
			warnings = append(warnings, key+"="+value)
		},
	)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(warnings) != 2 {
		t.Fatalf("warnings: got %d want 2: %v", len(warnings), warnings)
	}
}

func TestParse(t *testing.T) {
	defaults := labelquery.Defaults{CPU: 2, Arch: "x64"}

	tests := []struct {
		name    string
		labels  []string
		want    labelquery.RunnerSpec
		wantErr bool
	}{
		{
			name: "full spec",
			labels: []string{
				"runs-on=12345",
				"cpu=8",
				"arch=arm64",
				"pool=spot",
				"cache=s3",
				"image=ghcr.io/org/runner:v1",
			},
			want: labelquery.RunnerSpec{
				RunID: "12345",
				CPU:   8,
				Arch:  "arm64",
				Pool:  "spot",
				Cache: true,
				Image: "ghcr.io/org/runner:v1",
				Raw:   map[string]string{},
			},
		},
		{
			name: "defaults for missing cpu and arch",
			labels: []string{
				"runs-on=99",
			},
			want: labelquery.RunnerSpec{
				RunID: "99",
				CPU:   2,
				Arch:  "x64",
				Raw:   map[string]string{},
			},
		},
		{
			name: "unknown keys pass through to raw",
			labels: []string{
				"runs-on=1",
				"gpu=a100",
				"region=eu-west",
			},
			want: labelquery.RunnerSpec{
				RunID: "1",
				CPU:   2,
				Arch:  "x64",
				Raw: map[string]string{
					"gpu":    "a100",
					"region": "eu-west",
				},
			},
		},
		{
			name: "mem override",
			labels: []string{
				"runs-on=1",
				"mem=16Gi",
			},
			want: labelquery.RunnerSpec{
				RunID: "1",
				CPU:   2,
				Arch:  "x64",
				Mem:   "16Gi",
				Raw:   map[string]string{},
			},
		},
		{
			name: "cache true variants",
			labels: []string{
				"runs-on=1",
				"cache=true",
			},
			want: labelquery.RunnerSpec{
				RunID: "1",
				CPU:   2,
				Arch:  "x64",
				Cache: true,
				Raw:   map[string]string{},
			},
		},
		{
			name: "missing runs-on",
			labels: []string{
				"cpu=4",
			},
			wantErr: true,
		},
		{
			name: "empty runs-on value",
			labels: []string{
				"runs-on=",
			},
			wantErr: true,
		},
		{
			name: "malformed cpu",
			labels: []string{
				"runs-on=1",
				"cpu=banana",
			},
			wantErr: true,
		},
		{
			name: "malformed arch",
			labels: []string{
				"runs-on=1",
				"arch=ppc64",
			},
			wantErr: true,
		},
		{
			name: "malformed cache",
			labels: []string{
				"runs-on=1",
				"cache=banana",
			},
			wantErr: true,
		},
		{
			name: "malformed label missing equals",
			labels: []string{
				"runs-on=1",
				"invalidlabel",
			},
			wantErr: true,
		},
		{
			name: "duplicate runs-on last wins",
			labels: []string{
				"runs-on=first",
				"runs-on=second",
			},
			want: labelquery.RunnerSpec{
				RunID: "second",
				CPU:   2,
				Arch:  "x64",
				Raw:   map[string]string{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := labelquery.Parse(tt.labels, defaults, nil)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Parse() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
