# Plugin drafts

Drafts written by `probe plugin-from-crd`, most of them by the CRD request workflow
(`.github/workflows/crd-request.yml`) from a "Plugin request from a CRD" issue. Nothing here
is embedded in the app: a draft is validated (`./build.sh check` runs `plugin-validate` on
every file) but has only the generator's guesses for health, and translations written by the
workflow (`probe plugin-i18n`: the built-in plugins' glossary, then GitHub Models) that nobody
has proofread. A draft merged here is listed in the catalog the apps browse
(`plugins/index.json`, `just plugin-index`), so review it before merging: a person who imports it
still sees what it reads and can change on the consent sheet, but the file is what you merged.

To promote a draft to a built-in plugin:

1. Review the health rules and their reasons, the findings and the cards; render the samples
   with `just probe plugin-render go/plugin/drafts/<id>.yaml -demo` from the repository root.
2. Check the samples: invented names only, one per health rule.
3. Proofread the translations (fr, de, es, it and uk, `i18n`) and add the ones the lint lists
   as missing: `just probe plugin-i18n go/plugin/drafts/<id>.yaml` fills what the built-in
   plugins already say, `-fill answer.json` applies yours; `go test ./plugin/...` checks them.
4. `git mv go/plugin/drafts/<id>.yaml go/plugin/builtin/`, add its catalog row and icon if it has
   none, then `just plugin-index` (the catalog the apps browse lists the drafts), `just plugin-check`
   and `go test ./plugin/... ./ichorgo/`.
5. `just probe plugin-lint go/plugin/builtin/<id>.yaml -crd MANIFEST -strict` passes.

The format is documented in `docs/plugins.md`.
