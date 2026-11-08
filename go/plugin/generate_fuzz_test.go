package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzGenerate: no CRD manifest, however broken, makes LoadCRD or Generate panic, and every
// draft Generate writes loads back.
func FuzzGenerate(f *testing.F) {
	files, err := filepath.Glob(filepath.Join("testdata", "crds", "*.yaml"))
	if err != nil {
		f.Fatal(err)
	}

	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			f.Fatal(err)
		}

		f.Add(src)
	}

	f.Fuzz(func(t *testing.T, src []byte) {
		crd, err := LoadCRD(src)
		if err != nil {
			return
		}

		out, err := Generate(crd, GenerateOptions{ID: "fuzz"})
		if err != nil {
			return
		}

		if _, err := Load("fuzz.yaml", out); err != nil {
			t.Errorf("a generated draft does not load: %v\n%s", err, out)
		}
	})
}
