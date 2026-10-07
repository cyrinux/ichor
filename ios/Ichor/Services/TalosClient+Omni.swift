import Foundation
import Ichorgo
import IchorCore

/// The "Enter details" form, and the sign-in of the Talos clusters reached through Omni (see
/// TalosClient for the conventions). `config` is the stored talosconfig, `context` a context of it.
extension TalosClient {
    /// A one-context talosconfig from the form (TalosForm.json()), imported like a pasted one.
    static func buildTalosconfig(_ form: TalosForm) async throws -> String {
        let formJSON = try form.json()
        return try await run { IchorgoBuildTalosconfig(formJSON, $0) }
    }

    /// How the Omni context `context` signs its requests.
    static func omniSignInInfo(config: String, context: String) async throws -> OmniSignInInfo {
        try await json { IchorgoOmniSignInInfo(config, context, $0) }
    }

    /// Signs `context` in with a service account key (base64, as Omni shows it).
    static func omniSetServiceAccount(config: String, context: String, key: String) async throws {
        try await run { error -> Void in _ = IchorgoOmniSetServiceAccount(config, context, key, error) }
    }

    static func omniSignOut(config: String, context: String) async throws {
        try await run { error -> Void in _ = IchorgoOmniSignOut(config, context, error) }
    }

    /// Starts the browser sign-in of `context` (the context needs an identity): the same events
    /// as a kubeconfig cluster's browser sign-in.
    static func startOmniSignIn(config: String, context: String)
        -> (events: AsyncStream<KubeSignInEvent>, complete: @Sendable (String) -> Void, cancel: @Sendable () -> Void) {
        signInRun { IchorgoStartOmniSignIn(config, context, $0) }
    }
}
