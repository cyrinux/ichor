package plugin

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sync"
)

// The built-in plugins, one YAML file per integration.
//
//go:embed builtin
var builtinFS embed.FS

// Builtins are the built-in plugins, compiled once, sorted by file name. A built-in that
// does not load or validate is a bug of the build: the first call panics, and the test suite
// makes that call.
func Builtins() []*Compiled { return builtins() }

var builtins = sync.OnceValue(func() []*Compiled {
	out, err := loadDir(builtinFS, "builtin")
	if err != nil {
		panic("plugin: built-in plugins: " + err.Error())
	}

	return out
})

// loadDir loads, validates and compiles every .yaml file of dir; ids must be unique.
func loadDir(fsys fs.FS, dir string) ([]*Compiled, error) {
	files, err := fs.Glob(fsys, path.Join(dir, "*.yaml"))
	if err != nil {
		return nil, err
	}

	out := make([]*Compiled, 0, len(files))
	seen := map[string]string{}

	for _, name := range files {
		src, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, err
		}

		c, err := CompileSource(name, src)
		if err != nil {
			return nil, err
		}

		id := c.Plugin.ID
		if other, dup := seen[id]; dup {
			return nil, fmt.Errorf("%s: id %q is already used by %s", name, id, other)
		}

		seen[id] = name
		out = append(out, c)
	}

	return out, nil
}
