import Foundation
import IchorCore

/// Each cluster's runbooks, as the Go core keeps them (sealed in its data directory, never in
/// the defaults): this only holds the list last read, so the screens update after an import or
/// a removal. Keyed by the cluster's context fingerprint, like the pins.
@Observable
@MainActor
final class RunbookStore {
    static let shared = RunbookStore()

    private(set) var items: [String: [RunbookItem]] = [:]

    /// The key Go keeps a cluster's runbooks under (Android adds the screenshot mode's; iOS
    /// keeps a single, real scope, as for the pins).
    static func scope(fingerprint: String) -> String { "runbooks.\(fingerprint)" }

    func list(for fingerprint: String) -> [RunbookItem] { items[fingerprint] ?? [] }

    func item(_ id: String, fingerprint: String) -> RunbookItem? { list(for: fingerprint).first { $0.id == id } }

    func reload(fingerprint: String) async throws {
        guard !fingerprint.isEmpty else { return }
        items[fingerprint] = try await TalosClient.runbooks(scope: Self.scope(fingerprint: fingerprint))
    }

    /// Keeps `yaml` once its plan was shown; never runs it.
    @discardableResult
    func importRunbook(_ yaml: String, fingerprint: String) async throws -> RunbookItem {
        let item = try await TalosClient.importRunbook(scope: Self.scope(fingerprint: fingerprint), yaml: yaml)
        try? await reload(fingerprint: fingerprint)
        return item
    }

    func remove(_ id: String, fingerprint: String) async throws {
        try await TalosClient.removeRunbook(scope: Self.scope(fingerprint: fingerprint), id: id)
        items[fingerprint]?.removeAll { $0.id == id }
    }

    func yaml(_ id: String, fingerprint: String) async throws -> String {
        try await TalosClient.runbookYAML(scope: Self.scope(fingerprint: fingerprint), id: id)
    }
}
