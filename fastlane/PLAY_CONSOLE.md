# Google Play Console: listing and policy answers

Package `name.levis.ichor`. The store listing itself (texts in six languages, icon,
feature graphic, screenshots) lives in `fastlane/metadata/android/`, in the layout
`fastlane supply` and IzzyOnDroid read. Regenerate the graphics with `scripts/play-assets.sh`.

Paste-in values for the Console forms that fastlane cannot upload follow.

## The Play build

`./build.sh play` (or `just build-play`) builds `app/build/outputs/bundle/play/app-play.aab`:
the `play` build type, i.e. the release build with

- **no self-updater** (Play's Device and Network Abuse policy): `BuildConfig.SELF_UPDATE` is
  false, so there is no Settings → Updates section, no update banner and no daily check, and
  `src/play/AndroidManifest.xml` removes `REQUEST_INSTALL_PACKAGES` and the install receiver;
- **no donation links** (Payments policy: only charities may take donations outside Play
  Billing): `BuildConfig.DONATIONS` is false, so the support card, the Sponsors link and the
  crypto addresses are hidden. The website keeps them.

CI builds it on every push (artifact `android-play`, with its R8 mapping). On a `v*` tag,
`release.yml` runs `fastlane supply`, once the `WIF_PROVIDER` and `SERVICE_ACCOUNT` secrets
are set: the bundle, its mapping and release notes go to the **internal** track, rolled out
to testers, and the store listing (texts, icon, feature graphic, screenshots) is updated
from this directory. Images are only re-uploaded when they changed. Set the repository
variable `PLAY_RELEASE_STATUS=draft` to get a draft instead.

Release notes ("What's new") are `<locale>/changelogs/<versionCode>.txt`. `just release-tag`
drafts the en-US file from the `feat`/`fix`/`perf` commits since the previous tag
(`scripts/play-notes.py`), opens it in `$EDITOR` and commits it before tagging (only
en-US: other locales are not generated). Empty the draft, or tag without
a terminal, and CI generates the en-US notes on the tag instead. Play's limit is 500
characters per language.

## One-time setup

1. **Create the app** in the Play Console (App name `Ichor for Talos Linux`, App, Free) and fill in
   the forms below.
2. **Play App Signing: choose "Use existing app signing key"** and upload the release key
   (Play's PEPK tool encrypts it). Play installs and GitHub/Obtainium installs then share one
   signature, so a user can move between them without uninstalling. With a Play-generated
   key, they could not. The same key also serves as the upload key, which the CI already
   signs with.
3. **Upload the first AAB by hand** (`android-play` artifact of the tag's run, or
   `ICHOR_KEYSTORE=… just build-play`): the API cannot create the first release.
4. **Service account for CI uploads (Workload Identity Federation, no JSON key):** Google
   Cloud → enable the *Google Play Android Developer API*, create a service account, a
   workload identity pool with a GitHub OIDC provider restricted to this repository
   (`assertion.repository == 'cyrinux/ichor'`), and grant the repository's principal
   *Workload Identity User* on the service account. Play Console → Users and permissions →
   invite the service account's email with *Release to testing tracks* and *Manage store
   presence* (listing and screenshots) for this app. Then:
   `gh secret set WIF_PROVIDER --body projects/<number>/locations/global/workloadIdentityPools/<pool>/providers/<provider>`
   and `gh secret set SERVICE_ACCOUNT --body <name>@<project>.iam.gserviceaccount.com`.
5. **Closed test** (new personal developer accounts): at least 12 testers opted in for 14
   days in a row before production access can be requested.
6. **Reviewer access:** the app does nothing without a talosconfig, and reviewers reject apps
   they cannot get into. See **App access** below.

## Main store listing

| Field | Value |
|---|---|
| App name | `Ichor for Talos Linux` |
| Short / full description | `fastlane/metadata/android/<locale>/` |
| App icon | `images/icon.png` (512×512) |
| Feature graphic | `images/featureGraphic.png` (1024×500) |
| Phone screenshots | `images/phoneScreenshots/` (7, 1200×2322 PNG) |
| Default language | English (United States), en-US |
| Translations | fr-FR, de-DE, es-ES, it-IT, uk |

The screenshots (ratio 1.94, under Play's 2:1 limit, and at least 1080 px wide) qualify for
featuring and recommendations. Their `N_` prefix sets the order `fastlane supply` uploads
them in. Locales without their own images use the en-US ones.

## Store settings

| Field | Value |
|---|---|
| App or game | App |
| Free or paid | Free |
| Category | Tools |
| Tags | Developer tools, Monitoring, Network tools (pick the closest offered) |
| Contact email | required, shown publicly: use a project address rather than a personal one |
| Website | `https://cyrinux.github.io/ichor/` |
| Privacy policy | `https://cyrinux.github.io/ichor/privacy.html` |

## App content

### Privacy policy

`https://cyrinux.github.io/ichor/privacy.html` (from `docs/privacy.html`, published
with the GitHub Pages site).

### Ads

No, the app does not contain ads.

### App access

"All or some functionality is restricted." The app only works against a Talos Linux cluster,
through a talosconfig (mTLS client certificate). There is no username or password.

Options, best first:

1. **A read-only talosconfig for a reachable demo cluster.** Give it as a QR code image URL
   and paste-able YAML in the instructions. Generate it with
   `talosctl config new talosconfig-review --roles os:reader --crt-ttl 2160h` and revoke it
   (let it expire, or rotate) after review.
2. **A built-in demo mode** that serves recorded data, which Play reviewers can open without
   a cluster. It also helps first-time users.

Instructions text (for option 1):

> Ichor is a client for Talos Linux server clusters and has no account. To review
> it: open the app, choose "Paste", paste the configuration from <URL>, and tap Import.
> Alternatively scan the QR code at <URL>. The configuration is read-only (os:reader) and
> connects to a demonstration cluster; reboot, shutdown and other operator actions are
> hidden for this role by design.

### Content rating (IARC questionnaire)

- Category: **All other app types** (utility, productivity, communication…).
- Violence, sexuality, language, controlled substances, gambling: **No** to all.
- Does the app allow users to interact or exchange content? **No.** Sharing a report through
  the system share sheet is not in-app user interaction.
- Does the app share the user's current location? **No.**
- Does the app allow purchases of digital goods? **No.**
- Is the app a web browser or search engine? **No.**

Expected rating: Everyone / PEGI 3 / USK 0.

### Target audience and content

- Target age groups: **18 and over** (a server administration tool). Not appealing to
  children.

### News app

No.

### Health apps, financial features, government apps

None of them.

### Data safety

Basis: the app has no backend. Cluster data goes straight to the user's own cluster. The AI
report goes to a provider the user picked, with their own key, only when they tap Ask. The one
silent transfer is Google ML Kit's diagnostics
([disclosure](https://developers.google.com/ml-kit/android-data-disclosure)), which must be
declared because SDK collection counts as the app's.

**Data collection and security**

| Question | Answer |
|---|---|
| Does your app collect or share any of the required user data types? | **Yes** (ML Kit diagnostics) |
| Is all of the user data collected by your app encrypted in transit? | **Yes** |
| Do you provide a way for users to request that their data is deleted? | **No**. The developer holds none; ML Kit's per-install identifier is reset by uninstalling |

**Data types**

| Data type | Collected | Shared | Ephemeral | Required or optional | Purposes |
|---|---|---|---|---|---|
| App info and performance → **Diagnostics** | Yes | No | No | Required | Analytics |
| App info and performance → **Other app performance data** | Yes | No | No | Required | Analytics |
| Device or other IDs → **Device or other IDs** | Yes | No | No | Required | Analytics |

Everything else: **not collected**. Reasoning for the borderline cases:

- **Talosconfig, cluster state, logs** go only to servers the user owns and configured, not
  to the developer or a third party. They are not collected in Play's sense.
- **AI diagnosis**: transferred to a third party only on a specific user action (tapping Ask,
  after seeing the report), with a provider and key the user entered. Play exempts
  user-initiated transfers the user reasonably expects from the "shared" disclosure. The
  report is cluster telemetry, not one of Play's personal data types. If a reviewer pushes
  back, declare **App activity → Other user-generated content**, shared, optional, purpose
  App functionality.
- **Camera**: frames are decoded on the device and never leave it, so photos and videos are
  not collected.
- **GitHub requests** (Talos release list, release notes) send no user data; the IP address
  seen by any server is not declared as collection unless it is retained for the app's
  purposes.

### Permissions declarations

| Permission | Declaration needed | Note |
|---|---|---|
| `INTERNET`, `POST_NOTIFICATIONS` | No | |
| `CAMERA` | No form; covered by the privacy policy | QR code import |
| `REQUEST_INSTALL_PACKAGES` | No | removed from the Play build |
| `<queries>` for `io.kubenav.kubenav` | No | a single named package, not `QUERY_ALL_PACKAGES` |

### Foreground services

None of the app's own. WorkManager merges `FOREGROUND_SERVICE` and its
`SystemForegroundService`, without a `foregroundServiceType` or any `FOREGROUND_SERVICE_*`
permission, and the background checks never run in the foreground, so the Console asks for
no foreground service declaration.

## Release

- Release name: `0.6.0`, versionCode `56` (the commit count at the tag; `scripts/version.sh`).
- Release notes: `fastlane/metadata/android/<locale>/changelogs/56.txt`. For each later
  release, add `<versionCode>.txt` per locale (500 characters max); CI falls back to
  `default.txt` and uploads none for a locale that has neither.
- Every tag: CI rolls the bundle out on the internal track and updates the listing; promote
  it to closed testing or production in the Console. By hand, the same upload is:

```sh
fastlane supply --package_name name.levis.ichor --json_key play-service-account.json \
  --aab app/build/outputs/bundle/play/app-play.aab --track internal --release_status completed \
  --metadata_path fastlane/metadata/android --sync_image_upload true
```
