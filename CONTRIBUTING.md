# Contributing to Ichor

Thanks for helping. Bug reports, fixes and features are welcome; for anything large, open
an issue first so we can agree on the approach before you spend time on it.

## Before you open a pull request

- Run `./build.sh check` (gofmt, vet, Go and Kotlin unit tests); CI runs the same.
- Keep a pull request to one change, with a commit message saying why, not only what.
- Don't add dependencies under a copyleft license that would apply to the whole app
  (GPL, AGPL, LGPL for statically linked code); ask in the issue first if unsure.
- Never commit keystores, credentials, cluster configs or other secrets.

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
[GitHub's private vulnerability reporting](https://github.com/cyrinux/ichor/security/advisories/new).
