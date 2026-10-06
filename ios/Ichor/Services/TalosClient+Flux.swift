import Foundation
import IchorCore
import Ichorgo

extension TalosClient {
    /// The Flux Kustomizations, HelmReleases and sources of every namespace (os:admin), read
    /// through their custom resources.
    func flux() async throws -> FluxStatus {
        try await Self.json { [config, context, kubeServer] in IchorgoKubeFlux(config, context, kubeServer, $0) }
    }

    /// Runs action on the Flux object target (os:admin): the annotation or the spec.suspend
    /// merge patch the flux CLI writes, which the controller picks up.
    func fluxAction(_ action: FluxAction, on target: FluxRef) async throws {
        try await Self.run { [config, context, kubeServer] error -> Void in
            _ = IchorgoKubeFluxAction(config, context, kubeServer, target.kind, target.namespace, target.name, action.rawValue, error)
        }
    }

    /// What reconciling the Flux object target now would change, object by object (os:admin,
    /// read only: server-side apply dry runs). Not cached: always fresh.
    func fluxDiff(of target: FluxRef) async throws -> FluxDiff {
        try await Self.json { [config, context, kubeServer] in
            IchorgoKubeFluxDiff(config, context, kubeServer, target.kind, target.namespace, target.name, $0)
        }
    }
}
