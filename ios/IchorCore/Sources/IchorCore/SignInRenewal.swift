import Foundation

// Renewing every sign-in ahead of need (Go kube_auth_renew.go, KubeRenewSignIns): on foreground
// and before each background check. Same rules as Android (monitor/SignInAlerts.kt).

/// One cluster's proactive renewal: `kind` "kube" or "talos" (an Omni context, reported only).
public struct SignInRenewal: Codable, Equatable, Sendable {
    public static let valid = "valid"
    public static let renewed = "renewed"
    public static let signInRequiredStatus = "signInRequired"
    public static let error = "error"

    public let context: String
    public let kind: String
    public let method: String
    public let status: String
    /// The current token's end and when a new sign-in will be needed, unix seconds; 0 unknown.
    public let expires: Int64
    public let sessionExpires: Int64
    public let reason: String

    public init(
        context: String, kind: String = ContextKind.kube, method: String = "", status: String,
        expires: Int64 = 0, sessionExpires: Int64 = 0, reason: String = ""
    ) {
        self.context = context
        self.kind = kind
        self.method = method
        self.status = status
        self.expires = expires
        self.sessionExpires = sessionExpires
        self.reason = reason
    }

    private enum CodingKeys: String, CodingKey { case context, kind, method, status, expires, sessionExpires, reason }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        context = try c.field(.context, "")
        kind = try c.field(.kind, "")
        method = try c.field(.method, "")
        status = try c.field(.status, "")
        expires = try c.field(.expires, 0)
        sessionExpires = try c.field(.sessionExpires, 0)
        reason = try c.field(.reason, "")
    }

    /// Go kube_auth_gke_native.go gcpNativeRenewReason.
    public static let googleNativeReason = "google-native"

    /// The identity provider ended the session: only the user can sign in again.
    public var signInRequired: Bool { status == Self.signInRequiredStatus }
    /// `signInRequired`, but not Sign in with Google's hour-long token, which the app renews
    /// itself with Google's SDK on the cluster's screen.
    public var needsUser: Bool { signInRequired && reason != Self.googleNativeReason }
    /// Signed in and usable; `error` (offline, provider down) says nothing either way.
    public var signedIn: Bool { status == Self.valid || status == Self.renewed }

    /// KubeRenewSignIns' answer.
    public static func decodeList(_ json: String) throws -> [SignInRenewal] {
        try TalosJSON.decode([SignInRenewal].self, from: json)
    }
}

/// The key of the "sign in again" alert (one per cluster); told like a certificate alert.
public let signInAlertKey = "signin"

/// The "sign in again" alert, worded by the app (`title`, `text`).
public func signInAlert(title: String, text: String) -> Alert {
    Alert(key: signInAlertKey, title: title, text: text, problem: true)
}

/// What a run's renewals tell: `contexts`, the watched clusters to tell "sign in again" now;
/// `notified`, the fingerprints told so far, to keep for the next run.
public struct SignInAlertPlan: Equatable, Sendable {
    public let contexts: [ContextSummary]
    public let notified: Set<String>
}

/// Plans the "sign in again" alerts from `renewals` for the watched `contexts`, `notified` being
/// the fingerprints already told. A cluster is told once until it is signed in again (a renewal
/// that succeeds forgets it); one that could not be renewed for another reason (offline) keeps
/// what it had.
public func signInAlertPlan(renewals: [SignInRenewal], contexts: [ContextSummary], notified: Set<String>)
    -> SignInAlertPlan
{
    var tell: [ContextSummary] = []
    var next = notified
    for renewal in renewals {
        let isKube = renewal.kind == ContextKind.kube
        guard let context = contexts.first(where: { $0.name == renewal.context && $0.isKube == isKube }),
            !context.fingerprint.isEmpty
        else { continue }
        if renewal.signedIn {
            next.remove(context.fingerprint)
        } else if renewal.needsUser, !notified.contains(context.fingerprint) {
            next.insert(context.fingerprint)
            tell.append(context)
        }
    }
    return SignInAlertPlan(contexts: tell, notified: next)
}
