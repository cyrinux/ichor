# App Store Connect: declarations and review answers

Bundle `name.levis.ichor` (widget `name.levis.ichor.widget`, App Group `group.name.levis.ichor`).
This is the iOS counterpart of [PLAY_CONSOLE.md](PLAY_CONSOLE.md): what to request from Apple,
and paste-in answers for the App Store Connect forms. The build and upload themselves are in the
README (**iOS → App Store**): `scripts/ios-build.sh appstore`, run by `ios.yml` on `v*` tags.

## What the app declares in the build

| Where | What | Why |
|---|---|---|
| `Info.plist` `NSCameraUsageDescription` | camera | QR code import (VisionKit, on the device) |
| `Info.plist` `NSFaceIDUsageDescription` | Face ID | app lock, confirming reboot and shutdown |
| `Info.plist` `NSLocalNetworkUsageDescription` | local network | nodes on the Wi-Fi network, network search, Wake-on-LAN |
| `Info.plist` `UIBackgroundModes: fetch` + `BGTaskSchedulerPermittedIdentifiers` | background refresh | background alerts |
| `Info.plist` `ITSAppUsesNonExemptEncryption` | encryption | see [Export compliance](#export-compliance-read-before-the-first-submission) |
| entitlements `com.apple.security.application-groups` | App Group | the widget reads the last check |
| entitlements `com.apple.developer.networking.multicast` | **only with `ICHOR_IOS_MULTICAST=1`** | Wake-on-LAN broadcasts, see below |
| `ios/Ichor/PrivacyInfo.xcprivacy`, `ios/TalosWidget/PrivacyInfo.xcprivacy` | privacy manifests | see [Privacy manifest](#privacy-manifest) |

The usage strings are translated in `ios/Ichor/*.lproj/InfoPlist.strings`.

## One-time setup (Apple Developer portal)

1. **Apple Developer Program** membership (paid), as an individual or an organization. The
   seller name shown on the store is the account holder's legal name.
2. **Identifiers → App IDs**: register `name.levis.ichor` and `name.levis.ichor.widget`, both with
   the **App Groups** capability, and the App Group `group.name.levis.ichor`, assigned to both.
   Automatic signing (`scripts/ios-build.sh appstore`) creates the profiles; it cannot create
   restricted capabilities, hence this step.
3. **Request the multicast entitlement** (next section). It is optional: without it, everything
   works except Wake-on-LAN by broadcast.
4. **App Store Connect API key** for CI: Users and Access → Integrations → App Store Connect API,
   *Admin* role, then the `IOS_TEAM_ID`, `ASC_KEY_ID`, `ASC_ISSUER_ID` and `ASC_KEY_P8` secrets
   of the `release` environment (README, **iOS**).
5. **Create the app** in App Store Connect: Apps → + → New App, platform iOS, name
   `Ichor for Talos Linux`, primary language English (U.S.), bundle ID `name.levis.ichor`,
   SKU `ichor`, full access.

### Multicast entitlement (Wake-on-LAN broadcasts)

iOS refuses broadcast and multicast UDP from apps without
`com.apple.developer.networking.multicast`, a restricted entitlement Apple grants on request,
per team. The app sends Wake-on-LAN broadcasts through a BSD socket
(`ios/Ichor/Services/BroadcastSocket.swift`); without the entitlement that send fails and the
app tells the user to set a relay address or the node's own address, which need nothing.

**Request it** (account holder or admin): <https://developer.apple.com/contact/request/networking-multicast>.
Paste-in answers:

- *App name / bundle ID*: Ichor for Talos Linux, `name.levis.ichor`.
- *What will your app use multicast or broadcast for?*

  > Ichor manages Talos Linux server clusters the user owns. It sends Wake-on-LAN magic packets
  > to power on the user's own servers: a UDP broadcast of 102 bytes (port 9 by default) to the
  > broadcast address of the Wi-Fi network the phone is on, 3 times, only when the user taps
  > "Wake" on a powered-off node. The destination MAC address is the node's, which the app read
  > from the server while it was running. The app never listens to multicast or broadcast
  > traffic and does not browse Bonjour services.

- *Protocols*: Wake-on-LAN (magic packet over UDP, IPv4 broadcast). No mDNS/Bonjour, no SSDP.
- *Why unicast is not enough*: a powered-off machine has no IP address that answers ARP, so the
  packet has to reach its Ethernet segment by broadcast.

**Once approved:**

1. Developer portal → Identifiers → `name.levis.ichor` → **Additional Capabilities** tab →
   enable **Multicast Networking**. Automatic signing then puts it in the provisioning profile.
2. Build with it: `gh variable set IOS_MULTICAST --body 1` for the tag builds (`ios.yml`), or
   `ICHOR_IOS_MULTICAST=1 scripts/ios-build.sh appstore` locally.
3. Check the signed app carries it:
   `codesign -d --entitlements :- ios/build/Build/Products/Release-iphoneos/Ichor.app | grep multicast`.

Do not set `IOS_MULTICAST` before the approval: signing fails because the profile lacks the
entitlement. Sideloaded builds (unsigned IPA, AltStore/Sideloadly) never get it.

## App information

| Field | Value |
|---|---|
| Name | Ichor for Talos Linux |
| Subtitle | Talos Linux clusters, on your phone |
| Category | Primary **Developer Tools**, secondary **Utilities** |
| Price | Free, no in-app purchases (the App Store build has no donation links, Guideline 3.2.2) |
| Privacy policy URL | `https://cyrinux.github.io/ichor/privacy.html` |
| Support URL | `https://github.com/cyrinux/ichor/issues` |
| Marketing URL | `https://cyrinux.github.io/ichor/` |
| Copyright | the account holder's name and year |

Description, keywords and promotional text can reuse `fastlane/metadata/android/<locale>/`
(full and short descriptions); keywords are limited to 100 characters, for example
`talos,kubernetes,k8s,cluster,etcd,kubectl,sysadmin,devops,homelab,server`.

### Age rating

Answer **None** / **No** to every content question (violence, sexuality, profanity, drugs,
gambling, horror, medical, unrestricted web access, user-generated content). Result: **4+**.
The debug shell and logs show text from the user's own servers; that is not unrestricted web
access or user-generated content in Apple's sense.

## App Privacy (the "nutrition label")

App Store Connect → the app → **App Privacy** → Get Started → *Do you or your third-party
partners collect data from this app?* → **No, we do not collect data from this app.** The label
then reads **Data Not Collected**.

Why this is accurate (Apple: data is *collected* when it leaves the device in a way that lets
you or your third-party partners access it beyond what's needed to serve the request in real
time):

- there is no backend, no analytics and no crash-reporting SDK on iOS (VisionKit and LocalAuthentication run on the device);
- cluster data (talosconfig, logs, metrics, etcd snapshots) goes only to servers the user owns
  and configured;
- **AI diagnosis**: off by default, sent only when the user taps Ask, after seeing the report,
  to a provider and API key the user entered (Anthropic, OpenAI…). The developer is not party
  to it and the provider is not the developer's partner. If App Review disagrees, answer
  **Yes** and declare **User Content → Other User Content**: not linked to identity, not used
  for tracking, purpose **App Functionality**.
- GitHub requests (Talos releases, release notes) carry no user data.

Guideline 5.1.2(i) (sharing personal data with third-party AI) is met by the same flow: the user
picks the provider, enters their own key, sees the report and confirms before anything is sent;
the privacy policy names the providers.

## Privacy manifest

`PrivacyInfo.xcprivacy` (app and widget) declares no tracking, no collected data, and the
*required reason* APIs the code uses, including the linked Go core:

| Category | Reason | Used by |
|---|---|---|
| UserDefaults | `CA92.1` own settings, `1C8F.1` App Group | settings; widget snapshot |
| File timestamp | `C617.1` files in the app container | captures, support bundles; Go `stat` |
| System boot time | `35F9.1` elapsed time | the Go runtime's monotonic clock |

`scripts/ios-build.sh ipa` fails if a manifest is missing from the bundle. If App Store Connect
emails **ITMS-91053 (Missing API declaration)** after an upload, the mail names the category:
add it to `ios/Ichor/PrivacyInfo.xcprivacy` with the matching reason from Apple's
[required reason API list](https://developer.apple.com/documentation/bundleresources/describing-use-of-required-reason-api)
(for example `NSPrivacyAccessedAPICategoryDiskSpace` → `E174.1`), then upload a new build. The
SwiftTerm package ships its own manifest if it needs one.

## Export compliance (read before the first submission)

`Info.plist` sets `ITSAppUsesNonExemptEncryption` to **true**: the app does not only use
Apple's encryption. The Go core implements TLS (mTLS to Talos, HTTPS to Kubernetes and the AI
providers) and age (X25519 / ChaCha20-Poly1305) for encrypted etcd snapshots, with standard
algorithms of its own (Go's crypto library), not the operating system's. App Store Connect
therefore asks about encryption for each build until a compliance code is set (step 3).

Apple's current rules ([overview](https://developer.apple.com/help/app-store-connect/manage-app-information/overview-of-export-compliance),
[documentation](https://developer.apple.com/help/app-store-connect/reference/export-compliance-documentation-for-encryption)):

| Encryption used | US (EAR) | France |
|---|---|---|
| only Apple's (OS-provided) | nothing | nothing |
| **standard algorithms not provided by the OS** ← Ichor | exempt (mass market), self-classification | **French encryption declaration to upload** |
| proprietary / non-standard | CCATS | French declaration |

For an App Store release:

1. **Distributed in France** (the default when "all countries" is selected): file a
   *déclaration de fourniture d'un moyen de cryptologie* with ANSSI
   ([cyber.gouv.fr, contrôle d'un moyen de cryptologie](https://cyber.gouv.fr/reglementation/reglementation-identite-confiance-numerique/controles-reglementaires-cryptographie/controle-moyen-de-cryptologie/)):
   the form, signed, plus a short technical description (algorithms: TLS 1.2/1.3 with ECDHE,
   AES-GCM, ChaCha20-Poly1305; age with X25519 and ChaCha20-Poly1305; keys stored in the Secure
   Enclave/Keychain), by e-mail to the address on that page. ANSSI acknowledges it; that
   acknowledgment is the document App Store Connect asks for.
   **Until then**, exclude France in **Pricing and Availability**.
2. Answer the questions in App Store Connect (**App Information → App Encryption
   Documentation**, or on the first build in TestFlight): *standard encryption algorithms
   instead of, or in addition to, Apple's*; *available in France*: Yes with the ANSSI
   acknowledgment uploaded, or No while France is excluded.
3. Once approved, App Store Connect shows an export compliance code. Store it so later builds
   skip the questions: `gh variable set IOS_EXPORT_COMPLIANCE_CODE --body <code>` (tag builds),
   or `ICHOR_IOS_EXPORT_CODE=<code> scripts/ios-build.sh appstore` locally; the build adds it to
   `Info.plist` as `ITSEncryptionExportComplianceCode`. Make the documentation cover France
   again (step 1) before adding France back to the countries.

## App Review information

- **Sign-in required**: No. The import screen has **Try demo**, which opens a recorded,
  offline cluster with every screen populated.
- **Notes** (paste-in):

  > Ichor is a client for Talos Linux server clusters that the user runs; it has no account
  > and no backend. To review it without a cluster, tap "Try demo" on the first screen: it
  > loads an offline demonstration cluster. Reboot, shutdown and other actions are refused in
  > the demo by design. The local network permission is used to reach the user's servers on
  > their Wi-Fi network, search it for them, and send Wake-on-LAN packets to power them on.
  > Face ID protects the app and confirms disruptive actions. The optional AI diagnosis sends a
  > report the user reviewed first, to an AI provider with the user's own API key.

- **Contact**: name, phone and e-mail of the maintainer.

Things App Review may ask about, and the answers:

- **Guideline 2.5.2 (executing code)**: the debug shell and kubectl-like actions run on the
  user's servers, not on the phone; the app downloads no code.
- **Guideline 4.2 (minimum functionality)**: point to the demo and the feature list.
- **Background mode `fetch`**: background alerts about the user's clusters.

## Release

1. `just release-tag X.Y.Z --yes`, push the tag: `ios.yml` uploads the build to TestFlight.
2. App Store Connect → TestFlight: the build appears after processing (and after export
   compliance, if asked). Internal testers get it at once; external testers need a short beta
   review.
3. **App Store** tab → the version → select the build → *Add for Review* → *Submit*.
