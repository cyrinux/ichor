import Foundation
import Ichorgo
import IchorCore

enum ArgoSyncEvent: Sendable {
    case progress(ArgoSyncProgress)
    /// `result` is the operation's final phase; both empty after Cancel.
    case done(result: String, error: String?)
}

enum FluxReconcileEvent: Sendable {
    case progress(FluxReconcileProgress)
    /// `result` is ready, failed or "still reconciling"; both empty after Cancel.
    case done(result: String, error: String?)
}

enum HelmRollbackFollowEvent: Sendable {
    case progress(HelmRollbackFollowProgress)
    /// `result` is the final phase (Succeeded, Failed, Timeout); both empty after Cancel.
    case done(result: String, error: String?)
}

/// A followed rollback wait: its events and Cancel (stop following; the rollback stays done).
final class HelmRollbackFollowHandle: @unchecked Sendable {
    let events: AsyncStream<HelmRollbackFollowEvent>
    private let run: IchorgoHelmRollbackRun?

    fileprivate init(events: AsyncStream<HelmRollbackFollowEvent>, run: IchorgoHelmRollbackRun?) {
        self.events = events
        self.run = run
    }

    func cancel() { run?.cancel() }
}

/// A followed Argo CD sync: its events, Cancel (stop following; the sync goes on) and
/// Terminate (stop the sync itself; a network call, so off the main actor).
final class ArgoSyncHandle: @unchecked Sendable {
    let events: AsyncStream<ArgoSyncEvent>
    private let run: IchorgoArgoSyncRun?

    fileprivate init(events: AsyncStream<ArgoSyncEvent>, run: IchorgoArgoSyncRun?) {
        self.events = events
        self.run = run
    }

    func cancel() { run?.cancel() }

    func terminate() async throws {
        guard let run else { return }
        try await Task.detached { try run.terminate() }.value
    }
}

/// A followed Flux reconcile: its events and Cancel (stop following; the reconcile goes on).
final class FluxReconcileHandle: @unchecked Sendable {
    let events: AsyncStream<FluxReconcileEvent>
    private let run: IchorgoFluxReconcileRun?

    fileprivate init(events: AsyncStream<FluxReconcileEvent>, run: IchorgoFluxReconcileRun?) {
        self.events = events
        self.run = run
    }

    func cancel() { run?.cancel() }
}

/// Following a sync or a reconcile the phone just started (os:admin, reads only), until it
/// ends (see TalosClient for the conventions).
extension TalosClient {
    /// StartArgoSyncFollow: the Application namespace/name read every 3 s until its operation ends.
    func startArgoSyncFollow(namespace: String, name: String) -> ArgoSyncHandle {
        let (stream, continuation) = AsyncStream.makeStream(of: ArgoSyncEvent.self)
        let bridge = ArgoSyncBridge(
            progress: { continuation.yield(.progress($0)) },
            done: {
                continuation.yield(.done(result: $0, error: $1))
                continuation.finish()
            }
        )
        let run = IchorgoStartArgoSyncFollow(kubeConfig, kubeContext, kubeAPIServer, namespace, name, bridge)
        continuation.onTermination = { _ in
            _ = bridge  // keep the listener alive for the whole follow
        }
        if run == nil {
            continuation.yield(.done(result: "", error: String(localized: "Could not follow the sync.")))
            continuation.finish()
        }
        return ArgoSyncHandle(events: stream, run: run)
    }

    /// StartFluxReconcileFollow: the Flux object read every 3 s until the controller handled
    /// the reconcile request, for 10 minutes at most.
    func startFluxReconcileFollow(kind: String, namespace: String, name: String) -> FluxReconcileHandle {
        let (stream, continuation) = AsyncStream.makeStream(of: FluxReconcileEvent.self)
        let bridge = FluxReconcileBridge(
            progress: { continuation.yield(.progress($0)) },
            done: {
                continuation.yield(.done(result: $0, error: $1))
                continuation.finish()
            }
        )
        let run = IchorgoStartFluxReconcileFollow(kubeConfig, kubeContext, kubeAPIServer, kind, namespace, name, bridge)
        continuation.onTermination = { _ in
            _ = bridge  // keep the listener alive for the whole follow
        }
        if run == nil {
            continuation.yield(.done(result: "", error: String(localized: "Could not follow the reconcile.")))
            continuation.finish()
        }
        return FluxReconcileHandle(events: stream, run: run)
    }

    /// StartHelmRollbackFollow: the release's Deployments, StatefulSets and DaemonSets read
    /// every 3 s until they all run the rolled-back template, for 5 minutes at most (helm
    /// rollback --wait).
    func startHelmRollbackFollow(namespace: String, name: String) -> HelmRollbackFollowHandle {
        let (stream, continuation) = AsyncStream.makeStream(of: HelmRollbackFollowEvent.self)
        let bridge = HelmRollbackBridge(
            progress: { continuation.yield(.progress($0)) },
            done: {
                continuation.yield(.done(result: $0, error: $1))
                continuation.finish()
            }
        )
        let run = IchorgoStartHelmRollbackFollow(kubeConfig, kubeContext, kubeAPIServer, namespace, name, bridge)
        continuation.onTermination = { _ in
            _ = bridge  // keep the listener alive for the whole follow
        }
        if run == nil {
            continuation.yield(.done(result: "", error: String(localized: "Could not follow the workloads.")))
            continuation.finish()
        }
        return HelmRollbackFollowHandle(events: stream, run: run)
    }
}

private final class ArgoSyncBridge: NSObject, IchorgoArgoSyncListenerProtocol, @unchecked Sendable {
    private let progress: @Sendable (ArgoSyncProgress) -> Void
    private let done: @Sendable (String, String?) -> Void

    init(progress: @escaping @Sendable (ArgoSyncProgress) -> Void, done: @escaping @Sendable (String, String?) -> Void)
    {
        self.progress = progress
        self.done = done
    }

    func onProgress(_ json: String?) {
        guard let json, let decoded = try? TalosJSON.decode(ArgoSyncProgress.self, from: json) else { return }
        progress(decoded)
    }

    func onDone(_ result: String?, errMessage: String?) {
        done(result ?? "", errMessage.nonEmpty)
    }
}

private final class FluxReconcileBridge: NSObject, IchorgoFluxReconcileListenerProtocol, @unchecked Sendable {
    private let progress: @Sendable (FluxReconcileProgress) -> Void
    private let done: @Sendable (String, String?) -> Void

    init(
        progress: @escaping @Sendable (FluxReconcileProgress) -> Void,
        done: @escaping @Sendable (String, String?) -> Void
    ) {
        self.progress = progress
        self.done = done
    }

    func onProgress(_ json: String?) {
        guard let json, let decoded = try? TalosJSON.decode(FluxReconcileProgress.self, from: json) else { return }
        progress(decoded)
    }

    func onDone(_ result: String?, errMessage: String?) {
        done(result ?? "", errMessage.nonEmpty)
    }
}

private final class HelmRollbackBridge: NSObject, IchorgoHelmRollbackListenerProtocol, @unchecked Sendable {
    private let progress: @Sendable (HelmRollbackFollowProgress) -> Void
    private let done: @Sendable (String, String?) -> Void

    init(
        progress: @escaping @Sendable (HelmRollbackFollowProgress) -> Void,
        done: @escaping @Sendable (String, String?) -> Void
    ) {
        self.progress = progress
        self.done = done
    }

    func onProgress(_ json: String?) {
        guard let json, let decoded = try? TalosJSON.decode(HelmRollbackFollowProgress.self, from: json) else { return }
        progress(decoded)
    }

    func onDone(_ result: String?, errMessage: String?) {
        done(result ?? "", errMessage.nonEmpty)
    }
}
