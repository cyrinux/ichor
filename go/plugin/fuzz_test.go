package plugin

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// FuzzLoad checks that no file panics Load, Validate or Compile, and that a refused file is
// a *LoadError (the import screen shows its problems).
func FuzzLoad(f *testing.F) {
	files, _ := filepath.Glob("testdata/plugins/*.yaml")
	for _, name := range files {
		src, err := os.ReadFile(name)
		if err != nil {
			f.Fatal(err)
		}

		f.Add(src)
	}

	f.Add([]byte("schemaVersion: ichor.dev/plugin/v1alpha1\na: &a [*a]\n"))

	f.Fuzz(func(t *testing.T, src []byte) {
		p, err := Load("fuzz.yaml", src)
		if err != nil {
			var le *LoadError
			if !errors.As(err, &le) {
				t.Fatalf("error %T: %v", err, err)
			}

			return
		}

		Validate(p)
		_, _ = Compile(p)
	})
}
