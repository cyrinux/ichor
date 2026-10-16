# Built-in plugins

One YAML file per integration, embedded in the Go core and compiled once at first use
(`plugin.Builtins()`). A file that does not validate fails the test suite. The format is
documented in `docs/plugins.md`; `go/plugin/schema/plugin.schema.json` lets an editor check
it (`# yaml-language-server: $schema=../schema/plugin.schema.json`).
