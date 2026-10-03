import Foundation
import IchorCore
import Ichorgo

extension TalosClient {
    /// Health of Longhorn, Garage and CloudNativePG (os:admin). hints: their catalog ids seen in the
    /// inventory (see dataServiceHints); "" checks everything.
    func dataServices(hints: String) async throws -> DataServices {
        try await Self.json { [config, context, kubeServer] in IchorgoKubeDataServices(config, context, kubeServer, hints, $0) }
    }
}
