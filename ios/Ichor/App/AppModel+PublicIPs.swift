import Foundation
import IchorCore

// The public IP probe of the cluster on screen (see TalosClient.detectPublicIPs).
extension AppModel {
    /// The active cluster's public IP probe; none in screenshot mode (kept unmasked).
    var activePublicIPs: PublicIPReport? {
        settings.privacyMask ? nil : activeSummary.flatMap { publicIPReports[$0.fingerprint] }
    }

    var isDetectingPublicIPs: Bool { activeSummary.map { detectingPublicIPs.contains($0.fingerprint) } ?? false }

    /// Probes the active cluster's public IPs (see TalosClient.detectPublicIPs) and keeps the
    /// answer, unless screenshot mode came on meanwhile: Go then returned placeholders. Nil when
    /// a probe of this cluster is already running.
    func detectPublicIPs() async throws -> PublicIPReport? {
        guard let client, let fingerprint = activeSummary?.fingerprint, !fingerprint.isEmpty,
            !detectingPublicIPs.contains(fingerprint)
        else { return nil }
        detectingPublicIPs.insert(fingerprint)
        defer { detectingPublicIPs.remove(fingerprint) }
        let report = try await client.detectPublicIPs()
        guard !settings.privacyMask, summary?.contexts.contains(where: { $0.fingerprint == fingerprint }) == true else {
            return report
        }
        publicIPReports[fingerprint] = report
        try? PublicIPStore.save(publicIPReports)
        return report
    }
}
