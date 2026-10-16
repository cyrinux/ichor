import Foundation
import IchorCore

/// The screens pinned on each cluster's overview, kept like `@AppStorage("pins.<fingerprint>")`
/// (the standard defaults), in the JSON Android keeps too. Observed: a pin added on a screen
/// shows on the overview at once.
@Observable
@MainActor
final class PinStore {
    static let shared = PinStore()

    /// Bumped on every change, so views reading the pins update.
    private var revision = 0

    /// The pins of the cluster with this context fingerprint; none without one.
    func pins(for fingerprint: String) -> PinList {
        _ = revision
        guard !fingerprint.isEmpty else { return PinList() }
        return PinList.parse(UserDefaults.standard.string(forKey: PinList.storageKey(fingerprint: fingerprint)))
    }

    /// Pins `pin`'s screen, or unpins it when already pinned. False when nothing changed (full).
    @discardableResult
    func toggle(_ pin: Pin, fingerprint: String) -> Bool {
        let current = pins(for: fingerprint)
        let next = current.toggling(pin)
        guard next != current else { return false }
        save(next, fingerprint: fingerprint)
        return true
    }

    func remove(_ target: ShareTarget, fingerprint: String) {
        save(pins(for: fingerprint).removing(target), fingerprint: fingerprint)
    }

    /// Every cluster's pins: the stored clusters were deleted.
    func wipe() {
        let defaults = UserDefaults.standard
        for key in defaults.dictionaryRepresentation().keys where key.hasPrefix(PinList.storageKey(fingerprint: "")) {
            defaults.removeObject(forKey: key)
        }
        revision += 1
    }

    private func save(_ list: PinList, fingerprint: String) {
        guard !fingerprint.isEmpty else { return }
        let key = PinList.storageKey(fingerprint: fingerprint)
        if list.isEmpty {
            UserDefaults.standard.removeObject(forKey: key)
        } else {
            UserDefaults.standard.set(list.encoded, forKey: key)
        }
        revision += 1
    }
}
