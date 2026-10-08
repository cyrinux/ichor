import Foundation
import Ichorgo
import IchorCore

/// A stored kubeconfig context a Talos cluster's Kubernetes calls go through (K5).
struct KubeLink: Sendable, Equatable {
    /// The stored kubeconfig.
    let config: String
    let context: String
}

/// What an interactive sign-in reports.
enum KubeSignInEvent: Sendable {
    case prompt(KubeSignInPrompt)
    /// nil on success (and when cancelled).
    case done(error: String?)
}

/// Sign-ins of the clusters added from a kubeconfig, cloud discovery, and the Kubernetes
/// access of a Talos cluster (see TalosClient for the conventions). `kube` is the stored
/// kubeconfig, `context` a context of it.
extension TalosClient {
    /// The config the Kubernetes calls take: the linked kubeconfig, else the cluster's own.
    /// Talos calls, and those mixing Talos and Kubernetes (maintenance, upgrade, diagnosis,
    /// support bundle, the admin kubeconfig), keep `config`.
    var kubeConfig: String { kubeLink?.config ?? config }
    var kubeContext: String { kubeLink?.context ?? context }
    /// The address set for the Talos cluster does not apply to a linked kubeconfig.
    var kubeAPIServer: String { kubeLink == nil ? kubeServer : "" }

    /// How `context` signs in; nil for static credentials. `talos`: `kube` is the stored
    /// talosconfig and `context` an Omni context of it (the same goes for the calls below).
    static func signInInfo(kube: String, context: String, talos: Bool = false) async throws -> KubeSignInInfo? {
        let json = try await run { talos ? IchorgoTalosSignInInfo(kube, context, $0) : IchorgoKubeSignInInfo(kube, context, $0) }
        return try KubeSignInInfo.decode(json)
    }

    /// Signs `context` in with what the user entered (`secrets`: field → value JSON). An EKS
    /// IAM Identity Center sign-in throws kube-sign-in-required: then start the interactive one.
    static func setCredentials(kube: String, context: String, secrets: String, talos: Bool = false) async throws {
        try await run { error -> Void in
            _ = talos ? IchorgoTalosSetCredentials(kube, context, secrets, error) : IchorgoKubeSetCredentials(kube, context, secrets, error)
        }
    }

    static func signOut(kube: String, context: String, talos: Bool = false) async throws {
        try await run { error -> Void in _ = talos ? IchorgoTalosSignOut(kube, context, error) : IchorgoKubeSignOut(kube, context, error) }
    }

    /// A stored auth state as a backup keeps it (no session); "" when nothing is left.
    static func authForBackup(_ state: String) -> String {
        var error: NSError?
        let kept = IchorgoKubeAuthForBackup(state, &error)
        return error == nil ? kept : ""
    }

    /// The fields DiscoverClusters asks for, per provider.
    static func discoverFields() async throws -> [String: [String]] {
        let json = try await run { IchorgoKubeDiscoverFields($0) }
        return try decodeKubeDiscoverFields(json)
    }

    /// A kubeconfig of the clusters of a cloud account, for the import preview.
    static func discoverClusters(provider: String, secrets: String) async throws -> String {
        try await run { IchorgoDiscoverClusters(provider, secrets, $0) }
    }

    /// A running interactive sign-in: its events (finished after `done`), `complete` hands
    /// over a callback URL the app received, `cancel` stops waiting.
    typealias SignInRun = (events: AsyncStream<KubeSignInEvent>, complete: @Sendable (String) -> Void, cancel: @Sendable () -> Void)

    /// Starts the browser or device-code sign-in of `context`.
    static func startSignIn(kube: String, context: String, talos: Bool = false) -> SignInRun {
        signInRun { bridge in talos ? IchorgoStartTalosSignIn(kube, context, bridge) : IchorgoStartKubeSignIn(kube, context, bridge) }
    }

    /// Signs the account `email` in to the Omni instance at `endpoint` in the browser.
    static func startOmniSignIn(endpoint: String, email: String) -> SignInRun {
        signInRun { bridge in IchorgoStartOmniAccountSignIn(endpoint, email, bridge) }
    }

    /// Checks and stores an Omni service account key; the identity it signs as.
    static func omniServiceAccount(endpoint: String, key: String) async throws -> String {
        try await run { IchorgoSetOmniServiceAccount(endpoint, key, $0) }
    }

    /// A talosconfig of the clusters `identity` sees on the Omni instance, for the preview.
    static func discoverOmni(endpoint: String, identity: String) async throws -> String {
        try await run { IchorgoDiscoverOmniClusters(endpoint, identity, $0) }
    }

    private static func signInRun(_ start: (SignInBridge) -> IchorgoSignInRun?) -> SignInRun {
        let (stream, continuation) = AsyncStream.makeStream(of: KubeSignInEvent.self)
        let bridge = SignInBridge(
            prompt: { continuation.yield(.prompt($0)) },
            done: {
                continuation.yield(.done(error: $0))
                continuation.finish()
            }
        )
        let run = start(bridge)
        continuation.onTermination = { _ in
            run?.cancel()
            _ = bridge // keep the listener alive for the whole sign-in
        }
        return (stream, { run?.complete($0) }, { run?.cancel() })
    }
}

private final class SignInBridge: NSObject, IchorgoSignInListenerProtocol, @unchecked Sendable {
    private let prompt: @Sendable (KubeSignInPrompt) -> Void
    private let done: @Sendable (String?) -> Void

    init(prompt: @escaping @Sendable (KubeSignInPrompt) -> Void, done: @escaping @Sendable (String?) -> Void) {
        self.prompt = prompt
        self.done = done
    }

    func onPrompt(_ json: String?) {
        guard let json, let decoded = try? TalosJSON.decode(KubeSignInPrompt.self, from: json) else { return }
        prompt(decoded)
    }

    func onDone(_ errMessage: String?) {
        done(errMessage.nonEmpty)
    }
}
