package labelquery

import (
	"fmt"
	"strconv"
	"strings"
)

// RunnerSpec is the resolved runner configuration from workflow runs-on labels.
type RunnerSpec struct {
	RunID string
	CPU   int
	Mem   string
	Arch  string
	Pool  string
	Cache bool
	Image string
	Raw   map[string]string
}

// Defaults supplies fallback values when cpu/arch are omitted from labels.
type Defaults struct {
	CPU  int
	Arch string
}

// WarnFunc is called for unknown label keys passed through to Raw (SPEC §1).
type WarnFunc func(key, value string)

// Parse converts key=value label strings into a RunnerSpec.
// warn is optional; when set, unknown keys trigger a warning before storing in Raw.
func Parse(labels []string, defaults Defaults, warn WarnFunc) (RunnerSpec, error) {
	spec := RunnerSpec{
		CPU:  defaults.CPU,
		Arch: defaults.Arch,
		Raw:  make(map[string]string),
	}

	for _, label := range labels {
		key, value, ok := strings.Cut(label, "=")
		if !ok || key == "" {
			return RunnerSpec{}, fmt.Errorf("labelquery: malformed label %q", label)
		}

		switch key {
		case "runs-on":
			if value == "" {
				return RunnerSpec{}, fmt.Errorf("labelquery: runs-on value is required")
			}
			spec.RunID = value
		case "cpu":
			cpu, err := strconv.Atoi(value)
			if err != nil || cpu <= 0 {
				return RunnerSpec{}, fmt.Errorf("labelquery: invalid cpu value %q", value)
			}
			spec.CPU = cpu
		case "mem":
			if value == "" {
				return RunnerSpec{}, fmt.Errorf("labelquery: mem value is required")
			}
			spec.Mem = value
		case "arch":
			if value != "x64" && value != "arm64" {
				return RunnerSpec{}, fmt.Errorf("labelquery: invalid arch value %q", value)
			}
			spec.Arch = value
		case "pool":
			spec.Pool = value
		case "cache":
			enabled, err := parseCache(value)
			if err != nil {
				return RunnerSpec{}, err
			}
			spec.Cache = enabled
		case "image":
			spec.Image = value
		default:
			if warn != nil {
				warn(key, value)
			}
			spec.Raw[key] = value
		}
	}

	if spec.RunID == "" {
		return RunnerSpec{}, fmt.Errorf("labelquery: missing runs-on label")
	}

	return spec, nil
}

func parseCache(value string) (bool, error) {
	switch value {
	case "s3", "true", "1":
		return true, nil
	case "false", "0", "":
		return false, nil
	default:
		return false, fmt.Errorf("labelquery: invalid cache value %q", value)
	}
}
