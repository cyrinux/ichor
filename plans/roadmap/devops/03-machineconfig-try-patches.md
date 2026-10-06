# D3. Machineconfig patches in try mode

Status: **partial** (single-node edit and try mode done). Size L. Read [../README.md](../README.md) for the conventions.

## Done

- **Schema** (`configschema.go`): Talos's `config.schema.json` of the node's version, downloaded
  once from the Talos repository and kept in the data directory (`MachineConfigSchemaPrepare`).
- **Tree and field edits** (`configtree.go`, `configedit.go`): `MachineConfigDescribe` turns a
  config or a draft into a tree with the schema's types, documentation and allowed values;
  `MachineConfigEdit` applies one set/add/remove to a draft. Both local.
- **Preview and try** (`configapply.go`, `configsecrets.go`): `MachineConfigPreview` gives the
  redacted diff and whether a reboot is needed (`ApplyConfiguration` dry run);
  `StartConfigTry` applies in `TRY` mode with a 1, 5 or 10 minute timeout, then `Keep`
  (re-apply in `AUTO`) or `Revert`. Hidden secrets are put back from the node's config;
  changing or removing one is refused. Refused in privacy mode and when the node's config
  changed since the draft was made.
- **Apps**: the machine config screen has a Fields view and the YAML, an edit mode for both,
  a review of the diff and the try screen with its countdown.
- **Decision changed**: P1 below ("patches, not a YAML editor") was replaced by editing the
  config itself, field by field with the schema or as YAML: the schema makes fields safe to
  edit, and the diff shown before applying is what a patch would have been.

Still to do: staged and reboot modes (P3), several nodes (P5), snippets (P6), drift to edit
(P7), a countdown that survives leaving the screen (P4), probes against a real node.

## What exists today

- `machineconfig.go`: `NodeMachineConfig(config, ctx, node, revealSecrets)` reads the active
  config (COSI `config.MachineConfig`), secrets redacted unless revealed behind auth.
  Android `ui/machineconfig/MachineConfigScreen.kt` (search, copy, reveal), iOS `MachineConfigView.swift`.
- Config drift (`observations.go` `ClusterDriftSnapshot`/`CompareDrift`, Insights tab 0)
  compares effective values (DNS, NTP, MTU, extensions, Secure Boot, versions) but cannot fix anything.
- Idea source: `talosctl patch machineconfig --mode try --timeout 5m --patch '…'` applies without
  a reboot and reverts automatically unless re-applied.

## Goal

Change a node's (or several nodes') machine config from the phone **safely**: preview the diff,
apply in **try** mode with a countdown, check the cluster is still fine, then **Keep** (apply for
real) or let it **revert** by itself. A lost phone connection after a bad change heals itself.

## Key decisions

| # | Decision | Why |
|---|----------|-----|
| P1 | **Patches, not a YAML editor.** Input is a strategic merge patch (YAML) or a JSON merge patch, applied on the node's current config with the machinery `configpatcher` package (same as talosctl). | Editing a 300-line YAML on a phone is error-prone; patches are short and reviewable. |
| P2 | **Diff before apply**: Go returns a unified diff of the current vs patched config (secrets redacted on both sides) and the apply mode Talos would need (`ApplyConfiguration` with `DryRun: true` reports "no reboot / reboot required"). | The user sees what changes and whether it reboots. |
| P3 | **Try mode by default** (`Mode: TRY`, `TryModeTimeout` 1/5/10 min). Changes that need a reboot cannot be tried: offer "apply on next reboot" (`STAGED`) or "apply and reboot" with a typed-hostname confirm instead. | Try is the safety net; never surprise-reboot. |
| P4 | **Keep = re-apply the same patched config with `Mode: AUTO`** before the timeout. The countdown runs in a foreground service / background task; if the app dies, Talos reverts on its own. | That is how try mode works; nothing extra to clean up. |
| P5 | **Multi-node**: the patch is computed per node (each node's own base config), shown as a diff per node, applied node by node; a failure stops the rest. | Same as `talosctl -n a,b`, but stepwise. |
| P6 | **Snippet library** (built in, like `debugsnippets.go`): NTP servers, DNS nameservers, sysctl, kernel args (staged), MTU, kubelet extraArgs, apiserver extraArgs, registry mirror, a no-op label. Users can save their own (encrypted per cluster). | Most phone edits are these. |
| P7 | **Drift → patch**: a drift difference (for example NTP servers differ) gets "Fix with a patch…", pre-filled to make the node match the baseline. | Closes the loop with Insights. |
| P8 | **Gate**: `ApplyConfiguration` needs os:admin. Feature hidden below it. | Talos RBAC. |
| P9 | **Validation in Go before sending**: patch parses, result validates (`config.Validate` in mode "metal"/the node's mode), no change to `machine.ca`/`cluster.secret*`/`machine.token` (refused: rotating secrets from a phone is out of scope). | Prevents bricking a node by mistake. |

## Go

| File | Content |
|------|---------|
| `configpatch.go` | `MachineConfigPatchPreview(config, ctx, nodes, patch string) (json)`: per node `{diff, needsReboot, error}`; uses `machineConfigYAML` (revealSecrets internally, redacted diff out), `configpatcher.LoadPatches/Apply`, `ApplyConfiguration(DryRun)`. |
| `configapply.go` | `StartConfigApply(config, ctx, nodes, patch, mode string, timeoutSec int, listener) *ConfigApplyRun` (modes `try`, `auto`, `staged`, `reboot`); `ConfigKeep(…)` re-applies in AUTO; progress phases `applying`, `trying` (secondsLeft), `kept`, `reverted`, `failed`. Revert detection: watch `config.MachineConfig` version / `runtime.MachineStatus`. |
| `configsnippets.go` | built-in snippets with parameters. |
| tests | patch application on fixtures, forbidden-field refusal, multi-node stop on failure, try → keep / revert state machine (fake clock). |
| probe | `just probe patch-preview <node> <file>`, `just probe patch-try <node> <file> 60s`. |

## UI (both apps)

- Machine config screen: **Patch…** button → editor (monospace, snippet picker, node multi-select)
  → **Preview** (diff per node, reboot badge) → **Try for 5 min** / staged / reboot.
- Try run screen: big countdown ring, live node health (Ready, etcd, API reachable) under it,
  **Keep** and **Revert now** (revert = apply the previous config in AUTO). Ongoing notification
  with the countdown (U2).
- Insights drift row: "Fix with a patch…".

## Phases

1. Go preview + apply + keep, snippets, tests, probe. (L)
2. Android editor, preview, try screen. (M)
3. iOS. (M)
4. Drift → patch link. (S)

## Open questions

1. Allow raw full-config replacement for experts? (Proposal: no.)
2. Save the user's own snippets per cluster or globally?
