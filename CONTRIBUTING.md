# Contributing to Ichor

Thanks for helping. Bug reports, fixes and features are welcome; for anything large, open
an issue first so we can agree on the approach before you spend time on it.

## Before you open a pull request

- Run `./build.sh check` (gofmt, vet, Go and Kotlin unit tests); CI runs the same.
- No source file over 800 lines (`just length-check`); split it by domain instead.
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
imports it and keeps a thin wrapper per entry point.

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
`ios/Ichor/Views/SharedComponents.swift` and `Components.swift` on iOS):

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
