import Foundation

// Talos clusters reached through Omni (Go omni_auth.go, omni_signin.go): what the core says
// about their sign-in, and which auth states the app keeps for them. Same rules as Android.

/// The code an error of the Go core starts with when an Omni cluster needs a key.
public let omniSignInRequiredCode = "omni-sign-in-required"

/// What follows the Omni sign-in code in a core error ("sign in to Omni: no key"), nil when
/// `message` is not about it. The code may come after a prefix a wrapping call added.
public func omniSignInRequiredReason(_ message: String) -> String? {
    guard let range = message.range(of: omniSignInRequiredCode) else { return nil }
    let rest = message[range.upperBound...].drop { $0 == ":" || $0 == " " }
    return String(rest)
}

/// Whether `message` (a core error) says the Omni cluster needs a key.
public func isOmniSignInRequired(_ message: String) -> Bool {
    omniSignInRequiredReason(message) != nil
}

/// How a stored Omni context signs its requests (Go OmniSignInInfo).
public struct OmniSignInInfo: Decodable, Equatable, Sendable {
    public let signedIn: Bool
    /// "omni-service-account" or "omni-browser" while signed in.
    public let method: String?
    /// The service account, or the identity the browser key signs as.
    public let user: String?
    /// The context's own identity (a browser sign-in needs one).
    public let identity: String?
    /// The Omni instance's address.
    public let instance: String
    /// When a browser key expires, Unix seconds (0: never, or not signed in).
    public let sessionExpires: Int64

    public init(signedIn: Bool = false, method: String? = nil, user: String? = nil, identity: String? = nil,
                instance: String = "", sessionExpires: Int64 = 0) {
        self.signedIn = signedIn
        self.method = method
        self.user = user
        self.identity = identity
        self.instance = instance
        self.sessionExpires = sessionExpires
    }

    private enum CodingKeys: String, CodingKey { case signedIn, method, user, identity, instance, sessionExpires }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        signedIn = try c.field(.signedIn, false)
        method = try c.decodeIfPresent(String.self, forKey: .method).flatMap { $0.isEmpty ? nil : $0 }
        user = try c.decodeIfPresent(String.self, forKey: .user).flatMap { $0.isEmpty ? nil : $0 }
        identity = try c.decodeIfPresent(String.self, forKey: .identity).flatMap { $0.isEmpty ? nil : $0 }
        instance = try c.field(.instance, "")
        sessionExpires = try c.field(.sessionExpires, 0)
    }

    /// Signed in with a service account key (rather than a browser key that expires).
    public var isServiceAccount: Bool { method == OmniSignInMethod.serviceAccount }

    /// A browser sign-in is possible: the context names the account's email.
    public var canSignInWithBrowser: Bool { !(identity ?? "").isEmpty }
}

/// How an Omni context is signed in (OmniSignInInfo.method).
public enum OmniSignInMethod {
    public static let serviceAccount = "omni-service-account"
    public static let browser = "omni-browser"
}

/// The clusters whose states the auth store keeps (keyed by fingerprint): every cluster added
/// from a kubeconfig, and the Talos clusters reached through Omni (their keys).
public func authStoreFingerprints(talos: [ContextSummary], kube: [ContextSummary]) -> [String] {
    kube.map(\.fingerprint) + talos.filter(\.isOmni).map(\.fingerprint)
}
