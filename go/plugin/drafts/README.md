# Plugin drafts

Drafts written by `probe plugin-from-crd`, most of them by the CRD request workflow
(`.github/workflows/crd-request.yml`) from a "Plugin request from a CRD" issue. Nothing here
is embedded in the app: a draft is validated (`./build.sh check` runs `plugin-validate` on
every file) but has no translations and only the generator's guesses for health.

To promote a draft to a built-in plugin:

1. Review the health rules and their reasons, the findings and the cards; render the samples
   with `just probe plugin-render go/plugin/drafts/<id>.yaml -demo` from the repository root.
2. Check the samples: invented names only, one per health rule.
3. Translate every label for fr, de, es, it and uk (`i18n`); `go test ./plugin/...` checks it.
4. `git mv go/plugin/drafts/<id>.yaml go/plugin/builtin/`, add its catalog row and icon if it has
   none, then `just plugin-check` and `go test ./plugin/... ./ichorgo/`.
5. `just probe plugin-lint go/plugin/builtin/<id>.yaml -crd MANIFEST -strict` passes.

The format is documented in `docs/plugins.md`.
