# Phase 3: iOS

Prerequisite: phase 1 merged. This can run in parallel with phase 2. Mirror the Android
behaviour and wording; keep the Android plan open next to this one.

## 1. IchorCore model: `ios/IchorCore/Sources/IchorCore/DataServices.swift`

- `public struct ... : Decodable, Equatable, Sendable` per wire type, with tolerant
  decoding (`decodeIfPresent ?? default`). Pattern: `Workloads.swift`.
- `ServiceHealth: String` and the other enums, with `init(from:)` falling back to `.unknown`.
- The same pure helpers as Android: `detected`, `summary(kind:)`, `worst`,
  `dataServiceHints(apps:)`.
- Tests: `ios/IchorCore/Tests/IchorCoreTests/DataServicesTests.swift`, with the same cases as
  Android's `DataServicesTest`. IchorCore stays English-only (it's tested on Linux:
  `just ios-test-linux`).

## 2. Client: `ios/Ichor/Services/TalosClient+DataServices.swift`

```swift
func dataServices(hints: String) async throws -> DataServices {
    try await Self.json { [config, context, kubeServer] in
        TalosmobileKubeDataServices(config, context, kubeServer, hints, $0)
    }
}
```

(Pattern: `workloads()` in `TalosClient.swift:188`.)

## 3. Views

- `ios/Ichor/Views/DataServicesSection.swift`: the Overview section. Insert it in
  `OverviewView.swift` after `AppsCard` (~line 57), with the same visibility rules as
  Android (`model.allows(.workloads)` + inventory hints).
- `ios/Ichor/Views/DataServicesView.swift`: a segmented `Picker` over detected systems
  (pattern: `KubernetesView.swift` `enum Tab`), `.refreshable`, and the server sheet.
- `LonghornList.swift`, `GarageStatusView.swift`, `CnpgList.swift`: one file per tab.
- Route: `case dataServices` in `ios/Ichor/App/IchorApp.swift` (~line 120), dispatched
  next to `.workloads` (~line 164).
- Labels for the IchorCore enums go in `ios/Ichor/App/Localization.swift`.

## 4. Strings

`ios/Ichor/Localizable.xcstrings`: every new key in en, fr, de, es, it, uk.
`scripts/check-translations.py` (`just i18n-check`) checks iOS too.

## Done when

- [ ] `just ios-test-linux` (or `just ios-test`) green, and `just i18n-check` green
- [ ] `just ios-build` builds
- [ ] The demo cluster shows the same states as Android
