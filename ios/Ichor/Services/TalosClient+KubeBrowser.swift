import Foundation
import Ichorgo
import IchorCore

/// What a port-forward reports (StartPortForward).
enum PortForwardEvent: Sendable {
    /// Listening on the phone's loopback address ("127.0.0.1:PORT").
    case ready(String)
    /// One connection failed (the pod refused the port); the forward goes on.
    case connectionError(String)
    /// Stopped; the error when it was not asked to.
    case done(error: String?)
}

/// The resource browser, YAML edit, Helm releases, pod log follow and port-forward, through the
/// Kubernetes API of the cluster: the kubeconfig's for a kubeconfig cluster, the one Talos issues
/// otherwise, with the API address the user set (see TalosClient for the conventions).
extension TalosClient {
    /// The listable resources of the cluster, CRDs included, at their preferred version.
    func apiResources() async throws -> KubeAPIResourceList {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in IchorgoKubeAPIResources(config, context, kubeServer, $0) }
    }

    /// One page of `resource` as the server's Table shows it; `namespace` nil for every
    /// namespace (and for a cluster-scoped resource); `token` "" for the first page.
    func resourcePage(_ resource: KubeAPIResource, namespace: String?, token: String,
                      limit: Int = kubePageSize) async throws -> KubeResourcePage {
        let ns = resource.namespaced ? namespace ?? "" : ""
        return try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubeResourcePage(config, context, kubeServer, resource.group, resource.version, resource.resource,
                                    ns, token, limit, $0)
        }
    }

    /// One object as YAML, without managedFields; a Secret's values only with `reveal`.
    func objectYAML(_ resource: KubeAPIResource, namespace: String, name: String, reveal: Bool) async throws -> String {
        try await Self.run { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] error -> String in
            IchorgoKubeObjectYAML(config, context, kubeServer, resource.group, resource.version, resource.resource,
                                  namespace, name, reveal, error)
        }
    }

    /// A Secret or ConfigMap key by key, with the pods using it. A Secret's value comes only for
    /// `key` ("" for none; refused in screenshot mode). Never cached.
    func configData(_ resource: KubeAPIResource, namespace: String, name: String, key: String) async throws -> KubeConfigData {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubeConfigData(config, context, kubeServer, resource.configDataKind, namespace, name, key, $0)
        }
    }

    /// One object summed up: conditions, owners and managers, metadata, spec highlights, events.
    func objectSummary(_ resource: KubeAPIResource, namespace: String, name: String) async throws -> KubeObjectSummary {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubeObjectSummary(config, context, kubeServer, resource.group, resource.version, resource.resource,
                                     namespace, name, $0)
        }
    }

    /// What saving `edited` would change (a dry run), as a diff.
    func objectUpdatePreview(_ resource: KubeAPIResource, namespace: String, name: String, edited: String) async throws -> KubeEditPreview {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubeObjectUpdatePreview(config, context, kubeServer, resource.group, resource.version, resource.resource,
                                           namespace, name, edited, $0)
        }
    }

    /// What applying `manifests` (multi-document YAML) would do, from server-side apply dry runs;
    /// `namespace` is used for a namespaced object that names none.
    func applyPreview(namespace: String, manifests: String) async throws -> KubeApplyResult {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubeApplyPreview(config, context, kubeServer, namespace, manifests, $0)
        }
    }

    /// Applies `manifests` with server-side apply (os:admin), each object on its own.
    func apply(namespace: String, manifests: String) async throws -> KubeApplyResult {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubeApply(config, context, kubeServer, namespace, manifests, $0)
        }
    }

    /// Saves `edited` as the object, refused when it changed since it was read.
    func updateObject(_ resource: KubeAPIResource, namespace: String, name: String, edited: String) async throws {
        try await Self.run { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] error -> Void in
            _ = IchorgoKubeObjectUpdate(config, context, kubeServer, resource.group, resource.version, resource.resource,
                                        namespace, name, edited, error)
        }
    }

    /// The schema help for `fieldPath` ("spec.template", "" for the kind itself) of the
    /// resource's kind, from the API server's OpenAPI v3. Read-only.
    func explain(_ resource: KubeAPIResource, fieldPath: String) async throws -> KubeExplain {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubeExplain(config, context, kubeServer, resource.group, resource.version, resource.kind, fieldPath, $0)
        }
    }

    /// What deleting the object would do: protection, finalizers, the objects it owns. Read-only.
    func objectDeletePreview(_ resource: KubeAPIResource, namespace: String, name: String) async throws -> KubeDeletePreview {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubeObjectDeletePreview(config, context, kubeServer, resource.group, resource.version, resource.resource,
                                           namespace, name, $0)
        }
    }

    /// Deletes the object with `propagation`; refused when it changed since `resourceVersion`
    /// (the preview's) was read, and for a protected object unless `force`.
    func deleteObject(_ resource: KubeAPIResource, namespace: String, name: String, propagation: KubeDeletePropagation,
                      resourceVersion: String, force: Bool) async throws {
        try await Self.run { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] error -> Void in
            _ = IchorgoKubeObjectDelete(config, context, kubeServer, resource.group, resource.version, resource.resource,
                                        namespace, name, propagation.rawValue, resourceVersion, -1, force, error)
        }
    }

    /// How many pods the object wants and runs (a Job: its parallelism).
    func objectScale(_ resource: KubeAPIResource, namespace: String, name: String) async throws -> KubeObjectScale {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubeObjectScale(config, context, kubeServer, resource.group, resource.version, resource.resource,
                                   namespace, name, $0)
        }
    }

    /// `kubectl scale` for any scalable object (a Job: its parallelism): returns a warning when a
    /// HorizontalPodAutoscaler manages the count ("" when none).
    func scaleObject(_ resource: KubeAPIResource, namespace: String, name: String, replicas: Int) async throws -> String {
        try await Self.run { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] error -> String in
            IchorgoKubeScaleObject(config, context, kubeServer, resource.group, resource.version, resource.resource, resource.kind,
                                   namespace, name, replicas, error)
        }
    }

    /// The PersistentVolumeClaims of `namespace` (nil for every one), with volume, pods and fill.
    func storage(namespace: String?) async throws -> KubeStorage {
        let ns = namespace ?? ""
        return try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in IchorgoKubeStorage(config, context, kubeServer, ns, $0) }
    }

    /// The Services of `namespace` (nil for every one), with addresses, ready endpoints and routes.
    func services(namespace: String?) async throws -> KubeServices {
        let ns = namespace ?? ""
        return try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in IchorgoKubeServices(config, context, kubeServer, ns, $0) }
    }

    /// The Jobs of `namespace` (nil for every one), failures first.
    func jobs(namespace: String?) async throws -> KubeJobs {
        let ns = namespace ?? ""
        return try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in IchorgoKubeJobs(config, context, kubeServer, ns, $0) }
    }

    /// The latest revision of each Helm release; `namespace` nil for every namespace.
    func helmReleases(namespace: String?) async throws -> HelmReleaseList {
        let ns = namespace ?? ""
        return try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in IchorgoKubeHelmReleases(config, context, kubeServer, ns, $0) }
    }

    /// One Helm release: values, notes, manifest and history.
    func helmRelease(namespace: String, name: String) async throws -> HelmReleaseDetail {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in IchorgoKubeHelmRelease(config, context, kubeServer, namespace, name, $0) }
    }

    /// What rolling the release back to `revision` (0: the previous one) would change, each
    /// change dry-run on the API server. Read-only.
    func helmRollbackPlan(namespace: String, name: String, revision: Int) async throws -> HelmRollbackPlan {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubeHelmRollbackPlan(config, context, kubeServer, namespace, name, revision, $0)
        }
    }

    /// Rolls the release back to `revision` (0: the previous one), as `helm rollback` does: a
    /// new revision with the older one's chart and values.
    func helmRollback(namespace: String, name: String, revision: Int) async throws {
        try await Self.run { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] error -> Void in
            _ = IchorgoKubeHelmRollback(config, context, kubeServer, namespace, name, revision, error)
        }
    }

    /// `kubectl logs -f --tail`: the last `tailLines`, then each new line, until the consuming
    /// task is cancelled or the container stops. `container` may be "" for a pod with one.
    func followPodLogs(namespace: String, pod: String, container: String, tailLines: Int = 200) -> AsyncStream<LogFollowItem> {
        TalosClient.bridged { continuation in
            let bridge = LogBridge(
                line: { continuation.yield(.line($0)) },
                done: {
                    continuation.yield(.done(error: $0))
                    continuation.finish()
                }
            )
            let run = IchorgoStartPodLogFollow(kubeConfig, kubeContext, kubeAPIServer, namespace, pod, container, tailLines, bridge)
            return BridgedRun(bridge) { run?.cancel() }
        }
    }

    /// `kubectl exec -it`: a terminal in `container` ("" for the pod's only one); an empty
    /// command runs bash, or sh. Events go to the listener (from Go threads).
    func startPodShell(namespace: String, pod: String, container: String, command: String, cols: Int, rows: Int,
                       listener: IchorgoDebugListenerProtocol) -> IchorgoDebugSession? {
        IchorgoStartPodShell(kubeConfig, kubeContext, kubeAPIServer, namespace, pod, container, command, cols, rows, listener)
    }

    /// `kubectl debug -it`: adds a debug container from `image` sharing `targetContainer`'s
    /// processes ("" for none), then a terminal on it. It stays in the pod until the pod is deleted.
    func startPodDebug(namespace: String, pod: String, targetContainer: String, image: String, cols: Int, rows: Int,
                       listener: IchorgoDebugListenerProtocol) -> IchorgoDebugSession? {
        IchorgoStartPodDebug(kubeConfig, kubeContext, kubeAPIServer, namespace, pod, targetContainer, image, cols, rows, listener)
    }

    /// `kubectl debug node/`: a privileged pod on `node` in `namespace`, in the host's
    /// namespaces, then nsenter for a root shell on the node; the pod is deleted when it ends.
    func startNodeDebug(node: String, namespace: String, image: String, cols: Int, rows: Int,
                        listener: IchorgoDebugListenerProtocol) -> IchorgoDebugSession? {
        IchorgoStartNodeDebug(kubeConfig, kubeContext, kubeAPIServer, node, namespace, image, cols, rows, listener)
    }

    /// Forwards a port of the phone's loopback address to `remotePort` of the pod until the
    /// consuming task is cancelled; nothing listens on the network the phone is on.
    func portForward(namespace: String, pod: String, remotePort: Int) -> AsyncStream<PortForwardEvent> {
        TalosClient.bridged { continuation in
            let bridge = PortForwardBridge(
                event: { continuation.yield($0) },
                done: {
                    continuation.yield(.done(error: $0))
                    continuation.finish()
                }
            )
            let run = IchorgoStartPortForward(kubeConfig, kubeContext, kubeAPIServer, namespace, pod, remotePort, bridge)
            return BridgedRun(bridge) { run?.stop() }
        }
    }
}

private final class PortForwardBridge: NSObject, IchorgoPortForwardListenerProtocol, @unchecked Sendable {
    private let event: @Sendable (PortForwardEvent) -> Void
    private let done: @Sendable (String?) -> Void

    init(event: @escaping @Sendable (PortForwardEvent) -> Void, done: @escaping @Sendable (String?) -> Void) {
        self.event = event
        self.done = done
    }

    func onReady(_ address: String?) {
        event(.ready(address ?? ""))
    }

    func onConnectionError(_ errMessage: String?) {
        event(.connectionError(errMessage ?? ""))
    }

    func onDone(_ errMessage: String?) {
        done(errMessage.nonEmpty)
    }
}
