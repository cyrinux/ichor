# Google Play Console: listing and policy answers

Package `name.levis.talosmobile`. The store listing itself (texts in six languages, icon,
feature graphic, screenshots) lives in `fastlane/metadata/android/`, in the layout
`fastlane supply` and IzzyOnDroid read. Regenerate the graphics with `scripts/play-assets.sh`.

Paste-in values for the Console forms that fastlane cannot upload follow.

## Before the first upload: blockers

These would get the release rejected or the app suspended.

1. **Self-updater (Device and Network Abuse policy).** An app distributed on Play may not
   update itself by any other means than Play. A Play build must leave out
   `update/UpdateManager`, the Settings → Updates section, and the
   `REQUEST_INSTALL_PACKAGES` permission, which on its own also needs a declaration Play
   only grants to app stores, file managers and the like. A `play` product flavor that strips
   them, with a manifest overlay using `tools:node="remove"`, keeps the GitHub build as it is.
2. **Android App Bundle.** Play only accepts AABs for new apps. `build.sh` produces per-ABI
   APKs; add a `./gradlew bundlePlayRelease` path (the ABI `splits` block is ignored for
   bundles, and Play splits by ABI itself).
3. **Play App Signing.** Play re-signs with its own key, so Play installs and GitHub/Obtainium
   installs cannot update each other. Uploading the existing release key as the app signing
   key ("Use existing key" when enrolling) keeps them compatible.
4. **Donation links (Payments policy), a risk rather than a certain rejection.** Play
   exempts donations to registered charities from Play Billing. Reviewers have rejected
   open-source apps whose in-app "donate to the developer" links and wallet addresses go
   around it. The safe route is to hide `DonateDialog`, the crypto QR codes and the GitHub
   Sponsors link in the `play` flavor, and keep them on the website.
5. **Reviewer access.** The app does nothing without a talosconfig, and Play reviewers
   reject apps they cannot get into. See **App access** below.
6. **New personal developer account.** It needs a closed test with at least 12 testers opted
   in for 14 days in a row before production access can be requested.

## Main store listing

| Field | Value |
|---|---|
| App name | `Talosdev Mobile` |
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
| Website | `https://cyrinux.github.io/talosdev-mobile/` |
| Privacy policy | `https://cyrinux.github.io/talosdev-mobile/privacy.html` |

## App content

### Privacy policy

`https://cyrinux.github.io/talosdev-mobile/privacy.html` (from `docs/privacy.html`, published
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

> Talosdev Mobile is a client for Talos Linux server clusters and has no account. To review
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
| `REQUEST_INSTALL_PACKAGES` | **Remove from the Play build** | see Blockers |
| `<queries>` for `io.kubenav.kubenav` | No | a single named package, not `QUERY_ALL_PACKAGES` |

### Foreground services

None declared in the manifest. The background checks run as WorkManager jobs, so no
foreground service type declaration is needed.

## Release

- Release name: `0.6.0`, versionCode `56` (the commit count at the tag; `scripts/version.sh`).
- Release notes: `fastlane/metadata/android/<locale>/changelogs/56.txt`. For each later
  release, add `<versionCode>.txt` per locale (500 characters max).
- Upload with fastlane, after creating the app and its first release by hand in the Console:

```sh
fastlane supply --package_name name.levis.talosmobile --json_key play-service-account.json \
  --aab app/build/outputs/bundle/playRelease/app-play-release.aab --track internal \
  --metadata_path fastlane/metadata/android
```
