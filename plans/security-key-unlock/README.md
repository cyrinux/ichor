# Security keys for the app lock (YubiKey over NFC / USB)

Status: **implemented** (Android, iOS). Part of the UX roadmap ([../roadmap/ux.md](../roadmap/ux.md)).

A YubiKey worn all day, or any FIDO2 key, as a way to open Ichor instead of the fingerprint /
Face ID, tapped on the phone (NFC) or plugged in; and, optionally, as what the stored configs are
sealed with, so that nothing reads them without a tap.

## Decisions

| # | Decision | Why |
|---|----------|-----|
| K1 | **CTAP2 straight to the key** with Yubico's SDKs (`yubikit-android` 3.2.1, `yubikit-swift` 1.4.0), not the platform passkey APIs. | No Google Play Services (the F-Droid route), no relying-party website to host asset links on, works on every FIDO2 key. Both SDKs are Apache-2.0. |
| K2 | **User presence only** (`userVerification: discouraged`, a non-empty allow list): a touch, never the key's FIDO PIN at unlock. | That is the point: no PIN to remember. The PIN is asked at most once, at enrolment, by keys that insist on it for new credentials. |
| K3 | **Two modes.** *Unlock*: the key is an alternative to fingerprint / PIN, a pure gate like today's lock. *Required*: the credential files are sealed a second time with a data key (DEK) wrapped by the key's hmac-secret output. | Unlock costs nothing when the key is lost. Required makes the data at rest unreadable without the key, which the fingerprint lock never did (the Keystore / Enclave key is not bound to authentication). |
| K4 | The wrapping secret is the **WebAuthn prf / CTAP hmac-secret output** for a fixed random salt per enrolment, through HKDF-SHA256 to an AES-256-GCM key; the DEK is wrapped once per enrolled key; the files get `[magic "IK\x02"][AES-GCM(DEK, inner ciphertext)]` on top of the Keystore / ECIES layer. | Deterministic per (credential, salt), stable across both SDKs (same salt transform), and a spare key is just another wrap of the same DEK. The inner layer stays, so nothing is weaker than before. |
| K5 | **Up to two keys** (one worn, a spare). Adding a key in the required mode wraps the DEK for it with a second tap; turning the requirement on taps every key once; turning it off taps one key, then unseals. | Enough for the use case; every tap is one touch. |
| K6 | The **DEK lives in memory for the life of the process**, not dropped on the 30 s relock. | The app keeps its state through relocks anyway; dropping it would only break writes (a kubeconfig token refreshed while locked) without protecting anything the process does not already hold. A cold start still needs a tap. |
| K7 | **Background alerts and the widget pause once the OS has closed the app** in the required mode, and Settings says so. The workers already treat an unreadable config as "skip, retry later". | The sealed files cannot be read with nobody there; that is the mode's purpose. |
| K8 | The **uv flag** of the assertion is recorded with each wrap; a tap whose uv state differs is refused with a message instead of a silent unwrap failure. | A key with `alwaysUv` (or one whose setting changed) derives the other CredRandom; naming the cause beats "wrong key". |
| K9 | **Lost every key** in the required mode: the lock screen's "I lost my security keys" wipes the stored configs (and the kubeconfig sign-ins) so they can be imported again. The lock itself cannot be turned off while a key is required, nor the last key removed. | The data is unreadable by design; the only honest exit is starting over. |
| K10 | A forgotten **device** PIN is out of scope: it locks the phone, not the app. | No app can offer a bypass for the OS lock. |

## Where

- Android: `app/.../security/SecurityKeys.kt` (records, DEK wrap, sealed file, assertion check; pure JVM, tested),
  `SecurityKeyClient.kt` (yubikit, NFC + USB), `SecurityKeyPrompts.kt` (the "tap your key" dialog),
  `Authenticator.kt` (dispatch), `AppLock.kt` (enrolment, DEK holder), `data/SecureStore.kt` (outer layer),
  `ui/settings/SecurityKeysSection.kt` (add, spare, remove, require).
- iOS: `IchorCore/SecurityKeys.swift` (records, authenticatorData head, sealed-blob layout; tested on Linux),
  `Services/SecurityKeyCrypto.swift` (CryptoKit), `SecurityKeyClient.swift` (yubikit-swift, NFC + wired),
  `SecurityKeyPrompts.swift`, `Authenticator.swift`, `SecureConfigStore.swift`, `Views/SecurityKeyViews.swift`.
  `project.yml` adds the package, the NFC tag entitlement, the FIDO AID, the 5Ci accessory protocol and the smart-card entitlement.

## Verification

- Unit: `SecurityKeysTest`, `AppLockTest` (Android); `SecurityKeysTests` (IchorCore).
- Manual, a YubiKey 5 NFC and a 5Ci: enrol in the unlock mode, relock, tap to open, fingerprint
  still works; a reboot confirmation by tap; a key not enrolled refused; switch to required, relaunch,
  the fingerprint button is gone, a tap loads the config, the Settings note about alerts shows; add a
  spare, remove the first, open with the spare; the lost-keys path wipes and returns to import.
- Not verified at the time of writing: FIDO2 over USB-C on iPhone through the SDK's USB smart-card
  connection (NFC and Lightning are the iOS paths that are).
