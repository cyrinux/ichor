package main

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"time"

	"github.com/cyrinux/ichor/go/plugin"
)

const (
	// indexFile is the index, relative to the repository root.
	indexFile = "plugins/index.json"
	// defaultIndexRef is the branch the published files are read from. The index and its files
	// land together on it, so an entry always resolves, unlike on a tag cut later.
	defaultIndexRef        = "main"
	defaultIndexMaintainer = "cyrinux/ichor"
	indexRawBase           = "https://raw.githubusercontent.com/cyrinux/ichor/"
)

// pluginIndex is `plugin-index [-check] [-ref REF] DIR...`: writes plugins/index.json, under the
// directory `just` runs from, from every plugin file of the directories, or with -check fails
// when the file is not what it would write. A file that does not validate stops it: leaving one
// out silently would publish a shorter list. generatedAt only moves when the list changes, so a
// regeneration of an unchanged list changes nothing.
func pluginIndex(args []string, stdout io.Writer) error {
	const usage = "usage: plugin-index [-check] [-ref REF] DIR [DIR ...]"

	fs := flag.NewFlagSet("plugin-index", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	check := fs.Bool("check", false, "fail when plugins/index.json is stale, write nothing")
	ref := fs.String("ref", defaultIndexRef, "the branch or tag the file URLs point at")

	if err := fs.Parse(args); err != nil || fs.NArg() == 0 {
		return fmt.Errorf("%s (%v)", usage, err)
	}

	entries, err := indexEntries(fs.Args(), *ref)
	if err != nil {
		return err
	}

	target := filepath.Join(argPath("."), filepath.FromSlash(indexFile))

	existing, readErr := os.ReadFile(target)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}

	data, err := indexBytes(entries, existing)
	if err != nil {
		return err
	}

	if !bytes.Equal(data, existing) {
		if *check {
			return fmt.Errorf("%s is stale: run just plugin-index", indexFile)
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}

		if err := os.WriteFile(target, data, 0o644); err != nil {
			return err
		}
	}

	return printJSON(stdout, map[string]any{"ok": true, "plugins": len(entries)})
}

// indexBytes is the file for entries, keeping existing's generatedAt when its list is the same.
func indexBytes(entries []plugin.IndexEntry, existing []byte) ([]byte, error) {
	index := plugin.Index{
		SchemaVersion: plugin.IndexSchemaVersion,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		Plugins:       entries,
	}

	var old plugin.Index
	if json.Unmarshal(existing, &old) == nil && reflect.DeepEqual(old.Plugins, entries) {
		index.GeneratedAt = old.GeneratedAt
	}

	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return nil, err
	}

	return append(data, '\n'), nil
}

// indexEntries are the entries of every *.yaml of dirs, sorted by id.
func indexEntries(dirs []string, ref string) ([]plugin.IndexEntry, error) {
	entries := []plugin.IndexEntry{}
	seen := map[string]string{}

	for _, dir := range dirs {
		if !filepath.IsLocal(dir) {
			return nil, fmt.Errorf("%s: give a directory inside the repository, relative to its root", dir)
		}

		files, err := filepath.Glob(filepath.Join(argPath(dir), "*.yaml"))
		if err != nil {
			return nil, err
		}

		for _, file := range files {
			rel := path.Join(filepath.ToSlash(dir), filepath.Base(file))

			entry, err := indexEntryOf(rel, ref)
			if err != nil {
				return nil, err
			}

			if first, dup := seen[entry.ID]; dup {
				return nil, fmt.Errorf("%s: the id %q is listed twice (also %s)", rel, entry.ID, first)
			}

			seen[entry.ID] = rel
			entries = append(entries, entry)
		}
	}

	slices.SortFunc(entries, func(a, b plugin.IndexEntry) int { return cmp.Compare(a.ID, b.ID) })

	return entries, nil
}

// indexEntryOf validates and lints the plugin file at rel, as plugin-lint does.
func indexEntryOf(rel, ref string) (plugin.IndexEntry, error) {
	p, c, problems := checkPluginFile(rel)
	if plugin.HasErrors(problems) {
		printProblems(os.Stderr, rel, problems)

		return plugin.IndexEntry{}, fmt.Errorf("%s does not validate: %w", rel, errProblems(problems))
	}

	if lint := plugin.Lint(p, nil); plugin.HasErrors(lint) {
		printProblems(os.Stderr, rel, lint)

		return plugin.IndexEntry{}, fmt.Errorf("%s does not lint: %w", rel, errProblems(lint))
	}

	src, err := os.ReadFile(argPath(rel))
	if err != nil {
		return plugin.IndexEntry{}, err
	}

	return plugin.NewIndexEntry(c, src, indexRawBase+ref+"/"+rel, defaultIndexMaintainer), nil
}
