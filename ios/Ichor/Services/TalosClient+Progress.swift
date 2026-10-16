import Foundation
import IchorCore
import Ichorgo

/// How far a slow read got, polled while a screen waits on it.
extension TalosClient {
    /// The ReadProgress op of a checkup (`checkup()`).
    static let checkupProgressOp = "checkup"
    /// The ReadProgress op of `dataServices(hints:)`.
    static let dataServicesProgressOp = "dataservices"
    /// The ReadProgress op of `supportedIntegrations(hints:)`.
    static let integrationsProgressOp = "integrations"
    /// The ReadProgress op of `networkPolicies()`.
    static let netPolicyProgressOp = "netpol"
    /// The ReadProgress op of `apiHealth()`.
    static let apiHealthProgressOp = "apihealth"

    /// How far the running read `op` on this client's cluster got; nil when it cannot be read.
    /// Off the main actor: in screenshot mode the result waits for the mask.
    func readProgress(_ op: String) async -> ReadProgress? {
        let json = try? await Self.run { [config = self.kubeConfig, context = self.kubeContext] in IchorgoReadProgress(config, context, op, $0) }
        return json.flatMap(ReadProgress.decode)
    }

    /// Runs `read`; when `watching` (nothing is on screen yet), hands `update` how far `op` got
    /// every 250 ms until it returns (a progress not running yet is skipped).
    @MainActor
    func withReadProgress<T>(
        _ op: String,
        watching: Bool,
        update: @escaping @MainActor (ReadProgress) -> Void,
        _ read: () async -> T
    ) async -> T {
        let poll: Task<Void, Never>? = watching ? Task { @MainActor in
            while !Task.isCancelled {
                try? await Task.sleep(for: .milliseconds(250))
                let progress = await self.readProgress(op)
                guard !Task.isCancelled else { return }
                if let progress, progress.running { update(progress) }
            }
        } : nil
        let result = await withTaskCancellationHandler {
            await read()
        } onCancel: {
            poll?.cancel()
        }
        poll?.cancel()
        return result
    }
}
