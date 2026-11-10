package compsettings

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lyricat/inua/internal/config"
)

// style.animations reaches the backend as the settings file has it: on by
// default, off when turned off; unset leaves the compositor's own.
func TestFromAnimations(t *testing.T) {
	if got := From(config.Config{}).Animations; got != nil {
		t.Errorf("unset: %v, want nil", *got)
	}
	for _, tc := range []struct {
		name, file string
		want       bool
	}{
		{"default", "", true},
		{"off", "[style]\nanimations = false\n", false},
		{"on", "[style]\nanimations = true\n", true},
	} {
		path := filepath.Join(t.TempDir(), "config.toml")
		if tc.file != "" {
			if err := os.WriteFile(path, []byte(tc.file), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		c, warnings, err := config.Load(path)
		if err != nil || len(warnings) > 0 {
			t.Fatalf("%s: %v %v", tc.name, err, warnings)
		}
		got := From(c).Animations
		if got == nil || *got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
			continue
		}
		// The backend's copy is its own.
		*got = !*got
		if config.AnimationEnabled(c.Style.Animations) != tc.want {
			t.Errorf("%s: changing the backend's value changed the settings", tc.name)
		}
	}
}
