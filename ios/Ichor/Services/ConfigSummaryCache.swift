import Foundation
import Ichorgo
import IchorCore

/// The parsed summaries of the stored configs (ConfigSummaryRecord), kept so that a launch
/// shows the home without parsing every context. A sealed item next to the configs
/// (SecureConfigStore.Item.summaryCache): same key, same security-key seal, this device only,
/// never synced or backed up. Only a cache: one that cannot be read or decoded is deleted and
/// ignored, and the configs alone decide whether the app can read what is stored.
/// Read from the launch and the background checks, written from the main actor, hence the lock.
final class ConfigSummaryCache: Sendable {
    static let shared = ConfigSummaryCache()

    /// The record last read or written, so that showing the same configs again writes nothing.
    private let last = Locked<ConfigSummaryRecord?>(nil)

    /// The stored record, nil when there is none or it is unreadable (then deleted).
    func load() -> ConfigSummaryRecord? {
        guard SecureConfigStore.isStored(.summaryCache) else { return remember(nil) }
        if let data = SecureConfigStore.load(.summaryCache), let record = ConfigSummaryRecord.decode(data) {
            return remember(record)
        }
        // Sealed with a security key not tapped yet: unreadable for now, not corrupt.
        let awaitingKey = SecurityKeyStore.required && SecurityKeySession.shared.currentDek == nil
        if !awaitingKey { SecureConfigStore.delete(.summaryCache) }
        return remember(nil)
    }

    /// The summary of the stored `text` of `kind`: `record`'s when Go finds the text unchanged
    /// under the current mask (nothing parsed), else parsed. Nil when it does not parse.
    func summary(
        _ kind: StoredConfigKind, of text: String, record: ConfigSummaryRecord?,
        generation: Int
    ) async -> (summary: ConfigSummary, cached: Bool)? {
        let known = record?.knownDigest(kind, generation: generation) ?? ""
        do {
            let output = try await TalosClient.parseIfChanged(kind, text, knownDigest: known)
            return try summaryFromIfChanged(output, kind: kind, record: record)
        } catch is ConfigSummaryCacheMiss {
            guard let output = try? await TalosClient.parseIfChanged(kind, text, knownDigest: "") else { return nil }
            return try? summaryFromIfChanged(output, kind: kind, record: nil)
        } catch {
            return nil
        }
    }

    /// Keeps the summaries of the configs now stored (nil: none of that kind). Called each time
    /// the shown configs change: import, removal, renewal, edit, backup restore, screenshot mode.
    /// `generation`: TalosClient.privacyGeneration() read before the parse that made the newest
    /// summary; when the mask changed since, nothing is written (shouldStore).
    /// A failed write (e.g. no security key tapped) only means the next launch parses.
    func store(
        talos: String?, talosSummary: ConfigSummary?, kube: String?, kubeSummary: ConfigSummary?, generation: Int
    ) {
        let talosDigest = talosSummary == nil ? "" : talos.map { IchorgoConfigDigest($0) } ?? ""
        let kubeDigest = kubeSummary == nil ? "" : kube.map { IchorgoConfigDigest($0) } ?? ""
        do {
            let record = try ConfigSummaryRecord(
                talosDigest: talosDigest, kubeDigest: kubeDigest, maskGeneration: generation,
                summaries: StoredSummaries(talos: talosSummary, kube: kubeSummary))
            let kept = SecureConfigStore.isStored(.summaryCache) ? current() : nil
            guard record.shouldStore(currentGeneration: TalosClient.privacyGeneration(), stored: kept) else { return }
            try SecureConfigStore.save(record.encoded(), item: .summaryCache)
            remember(record)
        } catch {
            SecureConfigStore.delete(.summaryCache)
            remember(nil)
        }
    }

    private func current() -> ConfigSummaryRecord? {
        last.get()
    }

    @discardableResult
    private func remember(_ record: ConfigSummaryRecord?) -> ConfigSummaryRecord? {
        last.set(record)
        return record
    }
}

extension TalosClient {
    /// Go's Parse…IfChanged for a store of `kind`: "" when `knownDigest` is the text's digest
    /// (nothing parsed), the summary JSON otherwise.
    static func parseIfChanged(_ kind: StoredConfigKind, _ yaml: String, knownDigest: String) async throws -> String {
        switch kind {
        case .talosconfig: try await run { IchorgoParseConfigIfChanged(yaml, knownDigest, $0) }
        case .kubeconfig: try await run { IchorgoParseKubeconfigIfChanged(yaml, knownDigest, $0) }
        }
    }

    /// What the screenshot mode's masking currently is (0: off); see PrivacyGeneration in Go.
    static func privacyGeneration() -> Int {
        IchorgoPrivacyGeneration()
    }
}
