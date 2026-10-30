# Contributing to Ichor

Thanks for helping. Bug reports, fixes and features are welcome; for anything large, open
an issue first so we can agree on the approach before you spend time on it.

## Before you open a pull request

- Run `./build.sh check` (gofmt, vet, staticcheck, dead code, the Go tests under the race
  detector, govulncheck, Kotlin unit tests, Android Lint); CI runs the same. On a host whose
  ThreadSanitizer refuses the address space (some arm64 kernels) the Go tests run without
  `-race`, with a warning; CI always races.
- Dead code (`just deadcode`, `scripts/deadcode.py` with `go tool deadcode`, pinned in
  `go/go.mod`): a Go function that neither a Go test nor the apps reach fails the check. The
  apps' gomobile calls count as callers by themselves (`Ichorgo.fooBar(` in Kotlin,
  `IchorgoFooBar(` in Swift, `.fooBar(` on a handle), so a new facade function needs no
  allowlist: call it from an app or a test in the same pull request, or delete it.
- The demo-mode smoke test (`DemoSmokeTest`, CI job `ui-smoke`) walks the demo cluster on an
  emulator; it runs on CI only (see docs/build.md to run it on an AVD). Four walks: overview →
  node → kernel log; overview → node → Containers → a container's log; overview → Settings
  (Android through the ⋮ menu where it sits by default; iOS launched with Settings as the bar's
  only icon, since iOS folds a phone's toolbar into an overflow without identifiers); the
  Kubernetes demo's home. A moved anchor
  moves its tag (`ui/components/SmokeTags.kt`), not the test. The iOS twin
  (`IchorUITests/DemoSmokeTests`) runs on macOS CI after the simulator build; its anchors are
  accessibility identifiers (`SmokeID` in `ios/Ichor/App/UITestLaunch.swift`), never labels.
- A govulncheck finding (`just vuln`) means a known vulnerability is reachable from the Go
  core: bump the module (or the Go toolchain for a standard library one). If the call path
  is not reachable in practice, say why in the PR with `govulncheck -show verbose ./...`.
- Android Lint (`:app:lintDebug`, `:wear:lintDebug`) runs against `app/lint-baseline.xml` and
  `wear/lint-baseline.xml`, the findings known when it was turned on: only a new one fails, with
  its file and line in the log and the report in `app/build/reports/lint-results-debug.html`
  (a CI artifact when the check fails). Fix the finding; a pull request must not grow a
  baseline (`git diff --stat '*/lint-baseline.xml'` shows nothing). To refresh one on purpose
  (an AGP bump that renames checks, a category emptied), delete it, run the check twice (the
  first run writes it and stops) and review the diff in the PR.
- Run `just ai-eval <provider> <model>` when touching a `*_prompt.go` (needs an API key, so not
  in CI); `go test ./ichorgo -run AIEval` keeps every case inside the phone model's window.
- No source file over 800 lines (`just length-check`); split it by domain instead.
- No `panic(` in Go outside tests (`scripts/check-panics.py`): return an error. `onPanic` is
  the last net, not the first; the few exceptions (init-time embedded files, re-panics) are
  listed with a reason in the script.
- `ios/Ichor/Views` is organised by feature folder, each mirroring Android `ui/` packages (the
  table is in `ios/Ichor/Views/README.md`): a new screen goes in the folder of its Android twin,
  a view several folders use goes in `Components/`.
- Swift sources follow `ios/.swift-format` (4 spaces, 120 columns): the macOS CI `ios` job runs
  `swift-format lint --strict` before the build and fails on any finding. Run `just ios-format`
  on a Mac (it uses the selected Xcode's swift-format) and commit the result. Keep a pure reformat
  in its own commit. A rule is turned off in the config only for a stated reason in the pull
  request; a newer Xcode on the runner may need a one-time reformat.
- `ios/IchorCore` builds in Swift 6 language mode with complete concurrency checking, on macOS
  (`Package.swift`) and on Linux (`just ios-test-linux` passes the same flags). Shared mutable
  state goes behind `Locked` (`Sources/IchorCore/Locked.swift`), the package's one
  `@unchecked Sendable`; a new `@unchecked Sendable` needs a comment saying why it is safe.
- The iOS app, widget, share and watch targets build with complete concurrency checking
  (`SWIFT_STRICT_CONCURRENCY: complete` in `ios/project.yml`), with no concurrency warning in
  the macOS CI log. `Locked` is public for them; a gomobile object crosses threads in a
  `GoObject`. `Task.detached` is not used (`offMainActor`, or `TalosClient.run` for a Go call that
  waits). A new `@unchecked Sendable`, `nonisolated(unsafe)` or `@preconcurrency` import needs a
  comment saying why it is safe.
- On iOS, preferences live in `SettingsStore` (`ios/Ichor/App/SettingsStore.swift`, in the
  environment next to the model), the app lock and its security keys in `LockStore`
  (`ios/Ichor/App/LockStore.swift`, likewise), the nodes' features and reachability in `NodesStore`
  (`ios/Ichor/App/NodesStore.swift`, likewise), each cluster's repository and proposals in
  `ClusterGitStore` (`ios/Ichor/App/ClusterGitStore.swift`, likewise); `AppModel` is the cluster
  store: configs, selection, per-cluster device settings.
- Keep a pull request to one change, with a commit message saying why, not only what.
- Don't add dependencies under a copyleft license that would apply to the whole app
  (GPL, AGPL, LGPL for statically linked code); ask in the issue first if unsure.
- Never commit keystores, credentials, cluster configs or other secrets.

## Package layout (Go)

`go/ichorgo` is the gomobile facade: the only package bound for the apps, so every exported
function the apps call lives there, with the privacy masking, the audit log and the demo
contexts. New transport or pure-logic code goes in its own package under `go/<domain>` (like
`go/plugin`, or `go/gitforge`, the Git forge transport) or `go/internal/<name>` (like
`go/internal/httpx`, the one outbound HTTP client), with its tests next to it; `ichorgo`
imports it and keeps a thin wrapper per entry point. `go/kube` is the Kubernetes transport
only (requests, errors, the client cache, the follow loop): no kubeconfig parsing, demo
data, masking or audit, which stay in the facade (`go/ichorgo/kube_alias.go` holds the
wrappers). New Kubernetes domain logic goes in `go/kube/<domain>`, with `go/kube/helm` as the
worked example: its tests answer from the fake API in `go/kube/kubetest` and do not import the
facade, whose functions come down to demo check, funnel, a call to `helm`, mask and audit, JSON.
One domain moves per pull request; see `go/kube/helm/doc.go`.

Every synchronous read funnel (`withKube`, `withSession`, `withNodeSession`) derives from the
cluster's `callParent`, and the apps call `CancelCalls` when they switch cluster. Mutations
keep a background parent (`kubeMutate`, `nodeAction`, the `*From` variants). A followed run
holds its cluster's scope (`defer holdCalls(…)()` after `onPanic`, guarded by
`callscope_guard_test.go`); screen-bound streams are listed in `screenBoundRuns`.

## Facade contract

The apps decode the Go core's JSON with their own Kotlin and Swift types, leniently, so a
renamed Go json tag would compile everywhere and show a default value. The golden fixtures in
`go/ichorgo/testdata/contract/` (written from the demo data by `contract_test.go`) guard the
responses the apps read most: `go test` fails when one of those Go types changes, and the app
tests (`ContractTest.kt`, `ContractTests.swift`) fail when Go writes a key a model does not
read. When you change one of those types, run `just contract-update`, then fix the Kotlin and
Swift models the red tests point at. A new fixture needs a row in both app tests
(`scripts/check-contract.py` checks it in `./build.sh check`).

## Confirming risky actions

Do the same on Android and iOS:

- **Typed confirmation** for anything irreversible or cluster-wide: the user types the
  hostname or object name (`HostnameConfirmDialog` on Android, `HostnameConfirmationSheet`
  on iOS). Examples: reboot, shutdown, etcd member removal, upgrade, drain, scale to zero,
  Argo CD rollback, deleting every cluster (type `DELETE`).
- **Plain confirmation** for a reversible action on one object, with the target in the title
  ("Restart kubelet on cp-1?"). Examples: service start/stop/restart, Argo CD terminate,
  rollout restart.
- When a safer way out exists (a backup before deleting), offer it in the dialog.
- Plugin actions declare theirs: `confirm: typedName` for destructive verbs (`delete`, a
  `stop` or `reset` subresource), `plain` for the rest (docs/plugins.md "Actions").

## UI components

Screens build on one small set, the same names on both apps (`ui/components/` on Android,
`ios/Ichor/Views/Components/SharedComponents.swift` and `Components.swift` on iOS):

- Health colours come only from the status tokens (ok / warn / bad / muted), never from the
  cluster's colour.
- One `StatusPill` per header or card for a named state; a `HealthDot` leads each row; a `Tag`
  is for categories (role, type, scope, QoS) and is not health-coloured unless the value itself
  is a problem. A tag with the same value on every row goes in the header, or on the exceptions.
- Numbers are `StatTile`s, not prose; usage is `UsageBar` or `MeterRow`, with the caller's
  thresholds. Lists use `ResourceRow`; an empty list shows `EmptyState`.
- A list of resources sits in inset cards like the home, never a bare `LazyColumn` with a
  `HorizontalDivider` after each row: on Android `cardRows` (one card) or `groupedCardRows`
  (one titled card per namespace or status) with `CardListPadding`; a titled block is a
  `SectionCard`. iOS gets the same from an inset-grouped `List { Section }`.
- An explanatory paragraph is an `InfoHint`; at most one filled button per screen, and row
  actions live in the row's menu or sheet.
- Errors are a whole-screen error or an inline section error, never a plain line of text.

## Measuring a cold start

Turn on Settings → Support → "Startup timing" (always on in a debug build), close the app,
then:

- **Android:** `adb shell am force-stop name.levis.ichor.foss && adb logcat -c && adb logcat -s Ichor.Startup`,
  then launch the app (or `adb shell am start -W -n name.levis.ichor.foss/name.levis.ichor.MainActivity`;
  the Play build is `name.levis.ichor`). Each line
  is `<step> +<ms since process start> (Δ<ms since the previous step>)`; the Go core's steps
  start with `go:`.
- **iOS:** `log stream --predicate 'subsystem == "name.levis.ichor" && category == "startup"'`
  on a Mac with the device attached (or Console, filtered on category `startup`). Each Go call
  is also a signpost interval for Instruments.
- **Go core alone:** `just probe overview -timing -context <context>` prints the config parse,
  the session (opened or reused), the first node answered, all nodes and the total on stderr;
  `just probe parse -timing` the config parse and the context count.

The trace carries step names, counts and durations only, but before posting numbers in an
issue, check that nothing identifies your clusters: write "~23 contexts" and "cluster B", not
a context name, a hostname or an address.

The Go core also keeps a diagnostics ring (`go/ichorgo/diag.go`): every failed exported call,
recovered panic and startup step, plus anything logged through `log/slog`, redacted on write
whatever the screenshot mode. Log a constant message and attributes that are numbers, bools,
durations, counts, status codes, kinds or verbs: never a context, node, namespace, object
name or URL. `just probe -data-dir DIR diag-tail [N]` prints it (DIR keeps it across runs),
`just probe -data-dir DIR diag-export /tmp/diag.zip` writes the zip the apps will share.

## Reproducible builds

Every release APK on GitHub can be rebuilt from its tag and must come out the same once the
signatures are stripped. The `verify` job of `release.yml` checks it on each tag; by hand, on
Linux x86_64 with Nix:

```
just verify-release v1.40.1            # the arm64-v8a APK
just verify-release v1.40.1 --abi x86_64 --keep
```

It downloads `ichor-<tag>-<abi>.apk` from the release (`gh` must be signed in), rebuilds the tag
from a clean checkout with `./build.sh release` in the pinned Nix shell, unsigned, and compares
both entry by entry: name, order, size, CRC-32, compression and timestamp. What signing adds is
left out on both sides: the v1 files (`META-INF/*.SF`, `*.RSA`, `*.DSA`, `*.EC`,
`MANIFEST.MF`) and the APK Signing Block of v2/v3. It prints `identical: N entries`, or lists
the differing entries and exits 1. Only the Nix shell gives a comparable build (the JDK,
Gradle, Go, SDK build-tools and NDK are pinned there); without `nix` the script refuses.

When it fails, keep both stripped APKs (`--keep`) and run `diffoscope` on them; the usual
causes are a value the build takes from the moment or the machine (a timestamp or a path in a
`BuildConfig` field or a resource, a non-pinned dependency). Fix the cause rather than leaving
the entry out, and say here what was fixed.

Fixed so far (the first `verify` run, on v1.41.0):
- `lib/*/libgojni.so`: `gomobile bind -trimpath` (`build.sh`), so the checkout's path is not in the Go core's
  code; and, found by the second run (v1.42.0), gomobile's `replace <module> => <dir>` that Go keeps in the build
  info: the bind runs from the fixed link `/tmp/ichor-gobind/go`, and `build.sh` fails if the library names
  any other directory (`go version -m`).
- `META-INF/version-control-info.textproto`: `vcsInfo.include = false` on the release build types; AGP
  wrote the commit from a clone but a stub from the git worktree `verify` builds in.
- `assets/changelog.json`: `generated_at` is the commit's time (or `SOURCE_DATE_EPOCH`), not the clock's
  (`scripts/changelog.py`).

## Dependency bumps

Bumps arrive as pull requests, once a week per ecosystem:

- **Dependabot** (`.github/dependabot.yml`): the GitHub Actions (SHA and version comment),
  the Android libraries and Gradle plugins in groups, and the Go modules (minor and patch
  together, majors one by one).
- **Flake update** (`.github/workflows/flake-update.yml`): `flake.lock`, on the
  `deps/flake-update` branch. The workflow opens the PR only when Settings → Actions → General
  allows GitHub Actions to create pull requests; otherwise the run fails with the link to open
  it by hand. A PR the workflow token opens starts no workflow: close and reopen it to run the
  checks.

A bump PR is merged only when the Android and iOS workflows are green on it. A Go bump that
needs `go mod tidy` fails its check and is finished by hand. Two things are bumped by hand
only: the Talos machinery (`github.com/siderolabs/*`, which follows the Talos versions the
clusters run) and the Swift packages in `ios/project.yml` (pinned with `exactVersion`;
Dependabot reads `Package.swift` only).

## Plugin requests

The "Plugin request from a CRD" issue form (`.github/ISSUE_TEMPLATE/crd-request.yml`) labels its
issues `crd-request`; a maintainer adds `crd-approved` to a request from outside the project
before the generator acts on it. GitHub silently drops a label the repository does not have,
so a fork creates them once (`crd-request:failed` marks a request the workflow refused):

```
gh label create crd-request --color 5319e7 --description "A plugin request from a CRD link; the generator workflow acts on it"
gh label create crd-approved --color 0e8a16 --description "A maintainer approved this request for the generator workflow"
gh label create crd-request:failed --color b60205 --description "The generator workflow refused or could not draft this request"
```

`.github/workflows/crd-request.yml` acts on a request opened by the owner, a member or a
collaborator at once, and on anyone else's when a maintainer adds `crd-approved` (remove and
add it again to rerun; the pull request is then updated, and its branch is overwritten, so
commit review fixes after the last rerun). The run reads the issue as it is when the label is
added: read it again just before approving. It fetches the CRD from a
public forge (`scripts/crd-request.py`) or, for a request that names the CRDs and gives no
link (the app's Request an integration), finds it by GitHub code search and takes the project's
home page as the website, writes `go/plugin/drafts/<id>.yaml` with
`probe plugin-from-crd`, lints it against the CRD, and opens a draft pull request on the branch
`crd-request/<issue>-<id>`. A CRD the app already reads gets a comment and the `duplicate`
label instead.

A pull request opened with `GITHUB_TOKEN` starts no checks, and this repository does not let
`GITHUB_TOKEN` open one (Settings → Actions → General). The workflow uses a GitHub App when
it is configured:

1. Create a GitHub App (no webhook) with the repository permissions Contents: read and write,
   Pull requests: read and write, Issues: read and write, and install it on this repository
   only.
2. Add the repository secrets `CRD_BOT_CLIENT_ID` (the App's client ID) and
   `CRD_BOT_PRIVATE_KEY` (a private key it generated).

Without them the workflow pushes the branch and comments the link to open the pull request by
hand. Promoting a draft to a built-in plugin is described in `go/plugin/drafts/README.md`.

## Code ownership

[@cyrinux](https://github.com/cyrinux) is the default owner for all repository files;
review requests are defined in [.github/CODEOWNERS](.github/CODEOWNERS).
To make code-owner approval mandatory, enable **Require review from Code Owners**
in the branch protection rule or ruleset for `main`.

## Licensing of contributions

Ichor is licensed under the [Apache License 2.0](LICENSE). By submitting a contribution,
you agree that:

1. It is licensed under the Apache License 2.0 (section 5 of the license), with no
   additional terms or conditions.
2. It may be distributed, unmodified or modified, as part of any build of Ichor, including
   paid or commercial builds such as the Google Play and App Store versions, and builds
   with paid in-app features. You are not owed any payment for it.
3. You have the right to submit it, as certified by the sign-off below.

### Sign-off (Developer Certificate of Origin)

Every commit must be signed off, certifying the
[Developer Certificate of Origin 1.1](https://developercertificate.org/):

```
Signed-off-by: Your Name <you@example.com>
```

`git commit -s` adds it. To fix a branch that is missing sign-offs:
`git rebase --signoff main` and force-push. Pull requests with unsigned commits are not
merged.

## Name and logo

Contributing doesn't grant rights to the Ichor name or logo; see [TRADEMARKS.md](TRADEMARKS.md).

## Security issues

Don't open a public issue for a vulnerability; use
[GitHub's private vulnerability reporting](https://github.com/cyrinux/ichor/security/advisories/new); see [SECURITY.md](SECURITY.md).
