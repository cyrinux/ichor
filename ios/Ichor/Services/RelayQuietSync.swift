import Foundation
import IchorCore

/// Keeps the relay's copy of a paired cluster's quiet hours (the alert rules) up to date. On iOS
/// the ntfy app shows the relay's pushes, so the phone cannot hold them back as it does its own
/// alerts: the relay does, for this device, inside the window it was told.
enum RelayQuietSync {
    /// Tells the relay of the cluster `fingerprint` the quiet hours its rule sets now, in the
    /// phone's zone when the rule names none, if that is not what it last took (`force`: asked
    /// anyway, after a pairing or to try a relay that was too old again). A call that fails
    /// changes nothing: the next background check that reaches the cluster asks again.
    static func run(client: TalosClient, fingerprint: String, force: Bool = false) async {
        guard let record = RelayStore.read(fingerprint) else { return }
        let wanted = AlertRulesStore.load().relayQuietHours(fingerprint, zone: TimeZone.current.identifier)
        guard force || record.needsQuiet(wanted) else { return }
        guard let applied = try? await client.relaySetQuiet(record.source, id: record.deviceId, quiet: wanted)
        else { return }
        // Read again: unpaired or paired anew meanwhile, the answer is not about what is stored.
        guard let current = RelayStore.read(fingerprint), current.deviceId == record.deviceId else { return }
        try? RelayStore.save(current.asked(quiet: wanted, applied: applied), for: fingerprint)
    }
}
