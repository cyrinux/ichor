import Foundation
import IchorCore
import Ichorgo

/// How far a slow read got, polled while a screen waits on it.
extension TalosClient {
    /// The ReadProgress op of a checkup (`checkup()`).
    static let checkupProgressOp = "checkup"

    /// How far the running read `op` on this client's cluster got; nil when it cannot be read.
    /// Off the main actor: in screenshot mode the result waits for the mask.
    func readProgress(_ op: String) async -> ReadProgress? {
        let json = try? await Self.run { [config = self.kubeConfig, context = self.kubeContext] in IchorgoReadProgress(config, context, op, $0) }
        return json.flatMap(ReadProgress.decode)
    }
}
