import Foundation

// Signing kubeconfig clusters in (Go kube_auth*.go, kube_discover.go): what the core says about
// a cluster's sign-in, the auth states the app keeps for it, and the hybrid Talos + kubeconfig
// access (K5). Same rules as Android.

/// The code an error of the Go core starts with when the cluster needs the user to sign in.
public let kubeSignInRequiredCode = "kube-sign-in-required"

/// What follows the sign-in code in a core error ("sign in to this cluster (eks): …"), nil when
/// `message` is not about a sign-in.
public func kubeSignInRequiredReason(_ message: String) -> String? {
    guard let range = message.range(of: kubeSignInRequiredCode) else { return nil }
    let rest = message[range.upperBound...].drop { $0 == ":" || $0 == " " }
    return String(rest)
}

/// Whether `message` (a core error) says the cluster needs a sign-in.
public func isKubeSignInRequired(_ message: String) -> Bool {
    kubeSignInRequiredReason(message) != nil
}

/// How a stored kubeconfig context signs in (Go KubeSignInInfo; nil for static credentials).
public struct KubeSignInInfo: Decodable, Equatable, Sendable {
    /// oidc, eks, gke, azure, digitalocean, rancher.
    public let method: String
    /// "browser" (OIDC, device code included) or "credentials" (fields to enter).
    public let kind: String
    /// The fields of the first option.
    public let fields: [String]
    /// The alternative field sets (EKS: IAM Identity Center, or access keys).
    public let options: [[String]]
    /// The non-secret fields of the last sign-in, shown again so renewing it only needs confirming.
    public let values: [String: String]
    public let signedIn: Bool
    public let user: String?
    /// When a new sign-in will be needed, Unix seconds (0: unknown).
    public let sessionExpires: Int64

    public init(method: String, kind: String, fields: [String] = [], options: [[String]] = [],
                values: [String: String] = [:], signedIn: Bool = false, user: String? = nil, sessionExpires: Int64 = 0) {
        self.method = method
        self.kind = kind
        self.fields = fields
        self.options = options
        self.values = values
        self.signedIn = signedIn
        self.user = user
        self.sessionExpires = sessionExpires
    }

    private enum CodingKeys: String, CodingKey { case method, kind, fields, options, values, signedIn, user, sessionExpires }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        method = try c.field(.method, "")
        kind = try c.field(.kind, "browser")
        fields = try c.field(.fields, [])
        options = try c.field(.options, [])
        values = try c.field(.values, [:])
        signedIn = try c.field(.signedIn, false)
        user = try c.decodeIfPresent(String.self, forKey: .user).flatMap { $0.isEmpty ? nil : $0 }
        sessionExpires = try c.field(.sessionExpires, 0)
    }

    public var isCredentials: Bool { kind == "credentials" }

    /// The field sets to choose from: the options, else the fields alone.
    public var fieldSets: [[String]] {
        if !options.isEmpty { return options }
        return fields.isEmpty ? [] : [fields]
    }

    /// The field set to show first: the one the last sign-in filled, else the first.
    public var rememberedOption: Int {
        fieldSets.firstIndex { set in set.contains { !(values[$0] ?? "").isEmpty } } ?? 0
    }

    /// The decoded answer of KubeSignInInfo: nil for "" (static credentials).
    public static func decode(_ json: String) throws -> KubeSignInInfo? {
        guard !json.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return nil }
        return try TalosJSON.decode(KubeSignInInfo.self, from: json)
    }
}

/// What an interactive sign-in asks the app to show (Go signInPrompt).
public struct KubeSignInPrompt: Decodable, Equatable, Sendable {
    /// "browser": open `url`, it comes back by itself. "device": show `userCode`, open `url`.
    public let kind: String
    public let url: String
    public let userCode: String?
    public let verificationURL: String?
    /// Where the browser comes back (a loopback address).
    public let redirectPrefix: String?
    /// Seconds the device code stays valid (0: unknown).
    public let expiresIn: Int

    public init(kind: String, url: String, userCode: String? = nil, verificationURL: String? = nil,
                redirectPrefix: String? = nil, expiresIn: Int = 0) {
        self.kind = kind
        self.url = url
        self.userCode = userCode
        self.verificationURL = verificationURL
        self.redirectPrefix = redirectPrefix
        self.expiresIn = expiresIn
    }

    private enum CodingKeys: String, CodingKey {
        case kind, url, userCode, redirectPrefix, expiresIn
        case verificationURL = "verificationUrl"
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kind = try c.field(.kind, "browser")
        url = try c.field(.url, "")
        userCode = try c.decodeIfPresent(String.self, forKey: .userCode)
        verificationURL = try c.decodeIfPresent(String.self, forKey: .verificationURL)
        redirectPrefix = try c.decodeIfPresent(String.self, forKey: .redirectPrefix)
        expiresIn = try c.field(.expiresIn, 0)
    }

    public var isDevice: Bool { kind == "device" }

    /// The page to open: the complete verification URL when given, else `url`.
    public var openURL: String {
        if isDevice, url.isEmpty, let verificationURL { return verificationURL }
        return url
    }
}

// MARK: - Fields

/// How a sign-in or discovery field is entered.
public enum KubeFieldInput: Equatable, Sendable {
    /// A secret on one line (SecureField).
    case secret
    /// A JSON document, pasted or picked from a file (GCP service account key).
    case json
    /// Plain text (IDs, URLs, regions).
    case plain
}

public func kubeFieldInput(_ field: String) -> KubeFieldInput {
    switch field {
    case "gcpServiceAccountJson", "gcpUserCredentialsJson": .json
    case "awsSecretAccessKey", "awsSessionToken", "azureClientSecret", "doApiToken", "rancherApiKey", "serviceAccountKey",
         "gcpOAuthClientSecret": .secret
    default: .plain
    }
}

/// GKE's "Sign in with Google" option (a build with Ichor's iOS client): a marker, never a
/// text field; its value tells the Go core which platform's sign-in runs.
public let kubeGoogleSignInField = "gcpGoogleSignIn"

/// What the GKE "Sign in with Google" option submits to the Go core (KubeSetCredentials):
/// the browser sign-in then starts.
public func kubeGoogleSignInSecretsJSON() -> String {
    kubeSecretsJSON(fields: [kubeGoogleSignInField], values: [kubeGoogleSignInField: "ios"])
}

/// The URL scheme the in-app web session closes on for a sign-in coming back to
/// `redirectPrefix`: its own custom scheme (Google's reversed client ID), else the app's
/// "ichor" (a loopback address is answered to Go itself, the session never sees it).
public func kubeCallbackScheme(of redirectPrefix: String?) -> String {
    guard let prefix = redirectPrefix, let colon = prefix.firstIndex(of: ":") else { return "ichor" }
    let scheme = prefix[..<colon].lowercased()
    let valid = scheme.first?.isLetter == true && scheme.allSatisfy { $0.isLetter || $0.isNumber || "+-.".contains($0) }
    return !valid || scheme == "http" || scheme == "https" ? "ichor" : scheme
}

/// Fields that may be left empty.
public func kubeFieldOptional(_ field: String) -> Bool {
    field == "awsSessionToken" || field == "gcpProjects" || field == "gcpOAuthRedirectUrl"
}

/// The JSON object of `values` for `fields` (trimmed, empty ones left out), as KubeSetCredentials
/// and DiscoverClusters take it.
public func kubeSecretsJSON(fields: [String], values: [String: String]) -> String {
    var secrets: [String: String] = [:]
    for field in fields {
        let value = (values[field] ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        if !value.isEmpty { secrets[field] = value }
    }
    let data = (try? JSONSerialization.data(withJSONObject: secrets, options: [.sortedKeys])) ?? Data("{}".utf8)
    return String(decoding: data, as: UTF8.self)
}

/// Whether every required field of `fields` has a value.
public func kubeFieldsComplete(_ fields: [String], values: [String: String]) -> Bool {
    fields.allSatisfy { kubeFieldOptional($0) || !(values[$0] ?? "").trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }
}

/// The providers cloud discovery offers, in the order shown (Go KubeDiscoverFields keys).
public let kubeDiscoverProviders = ["eks", "gke", "aks", "digitalocean", "rancher"]

/// KubeDiscoverFields' answer: the fields per provider.
public func decodeKubeDiscoverFields(_ json: String) throws -> [String: [String]] {
    try TalosJSON.decode([String: [String]].self, from: json)
}

/// KubeDiscoverOptions' answer: the field sets of the providers that take one of several
/// credentials (GKE: a service account key, or gcloud user credentials).
public func decodeKubeDiscoverOptions(_ json: String) throws -> [String: [[String]]] {
    try TalosJSON.decode([String: [[String]]].self, from: json)
}

/// How far a running cloud discovery got (Go DiscoverProgress): GKE with a Google account reads
/// many projects. `projects` is 0 while they are still being listed.
public struct DiscoveryProgress: Decodable, Equatable {
    public var running = false
    public var projects = 0
    public var scanned = 0
    public var clusters = 0

    public init(running: Bool = false, projects: Int = 0, scanned: Int = 0, clusters: Int = 0) {
        self.running = running
        self.projects = projects
        self.scanned = scanned
        self.clusters = clusters
    }

    /// The share of the projects read, nil while they are listed (or none are read).
    public var fraction: Double? {
        projects > 0 ? Double(min(scanned, projects)) / Double(projects) : nil
    }

    public static func decode(_ json: String) -> DiscoveryProgress? {
        try? TalosJSON.decode(DiscoveryProgress.self, from: json)
    }
}

/// A credential set that signs in in the browser before discovery (the OAuth client, Sign in
/// with Google): the discovery sheet does not run that sign-in yet, so it is not offered.
func kubeDiscoverNeedsSignIn(_ set: [String]) -> Bool {
    set.contains("gcpOAuthClientId") || set.contains(kubeGoogleSignInField)
}

/// The credentials `provider` takes, as field sets: its options, else its one set of fields.
public func kubeDiscoverOptionSets(provider: String, fields: [String: [String]], options: [String: [[String]]]) -> [[String]] {
    let sets = (options[provider] ?? []).filter { !$0.isEmpty && !kubeDiscoverNeedsSignIn($0) }
    if !sets.isEmpty { return sets }
    return fields[provider].map { [$0] } ?? []
}

/// The stored names of the contexts an import added that sign in through a method (`signIn`):
/// those the user kept, under the name the core gives a taken one unless it replaced the same
/// cluster. What discovery signs in with the account's credentials once saved.
public func importedSignInContexts(summary: ConfigSummary, selected: Set<Int>, replacing: Set<Int>,
                                   conflicts: [(index: Int, suggested: String)]) -> [String] {
    summary.contexts.indices.compactMap { index in
        let ctx = summary.contexts[index]
        guard selected.contains(index), ctx.problem == nil, ctx.signIn != nil else { return nil }
        if !replacing.contains(index), let conflict = conflicts.first(where: { $0.index == index }) {
            return conflict.suggested
        }
        return ctx.name
    }
}

/// The message KubeSetCredentials answers for a context that does not sign in with credentials:
/// discovery skips those quietly.
public func isNotCredentialsMethod(_ message: String) -> Bool {
    message.contains("does not sign in with credentials")
}

// MARK: - Auth states

/// The auth states the app keeps for Go's AuthStore: cluster fingerprint → state JSON, sealed
/// as one item. Pure operations; KubeAuthStore holds the sealed copy.
public enum KubeAuthMap {
    /// The map stored as `data`; empty for nil or anything unreadable.
    public static func decode(_ data: Data?) -> [String: String] {
        guard let data, let map = try? JSONDecoder().decode([String: String].self, from: data) else { return [:] }
        return map
    }

    public static func encode(_ map: [String: String]) -> Data {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        return (try? encoder.encode(map)) ?? Data("{}".utf8)
    }

    /// `map` with `key` set to `state`; "" (or a blank key) deletes / changes nothing.
    public static func saving(_ map: [String: String], key: String, state: String) -> [String: String] {
        guard !key.isEmpty else { return map }
        var out = map
        out[key] = state.isEmpty ? nil : state
        return out
    }

    /// Only the states of the clusters still stored.
    public static func keeping(_ map: [String: String], fingerprints: [String]) -> [String: String] {
        let known = Set(fingerprints.filter { !$0.isEmpty })
        return map.filter { known.contains($0.key) && !$0.value.isEmpty }
    }
}

// MARK: - Hybrid access (K5)

/// The stored kubeconfig context the Kubernetes calls of `talos` go through, nil when it uses
/// the admin kubeconfig Talos issues: a Talos cluster linked (`links`: Talos fingerprint → kube
/// fingerprint) to a kube cluster still stored (`kubeContexts`).
public func kubeAccessContext(of talos: ContextSummary?, links: [String: String], kubeContexts: [ContextSummary]) -> String? {
    // An Omni cluster's Kubernetes goes through Omni, with its sign-in: no link applies.
    guard let talos, !talos.isKube, !talos.demo, !talos.omni, !talos.fingerprint.isEmpty,
          let target = links[talos.fingerprint], !target.isEmpty else { return nil }
    return kubeContexts.first { $0.isKube && $0.fingerprint == target }?.name
}

/// The links whose Talos cluster and kube cluster are both still stored.
public func keepKubeAccess(_ links: [String: String], talos: [String], kube: [String]) -> [String: String] {
    let talosKnown = Set(talos.filter { !$0.isEmpty }), kubeKnown = Set(kube.filter { !$0.isEmpty })
    return links.filter { talosKnown.contains($0.key) && kubeKnown.contains($0.value) }
}

/// The clusters whose states the auth store keeps (keyed by fingerprint): every cluster added
/// from a kubeconfig, and the Talos clusters reached through Omni (their keys).
public func authStoreFingerprints(talos: [ContextSummary], kube: [ContextSummary]) -> [String] {
    var keys = kube.map(\.fingerprint)
    // An Omni sign-in is kept per identity and instance (authKey); older cores kept it under
    // the context's fingerprint, which the core moves to the auth key on first use.
    for context in talos where context.omni && !context.isKube {
        keys.append(context.fingerprint)
        if let authKey = context.authKey { keys.append(authKey) }
    }
    var seen = Set<String>()
    return keys.filter { !$0.isEmpty && seen.insert($0).inserted }
}

public extension ContextSummary {
    /// `allows`, for a Talos cluster whose Kubernetes access goes through a stored kubeconfig
    /// when `kubeLinked`: the Kubernetes screens then answer to that kubeconfig's RBAC, not the
    /// talosconfig's role. Talos features (the admin kubeconfig export included) keep the role.
    func allows(_ feature: Feature, kubeLinked: Bool) -> Bool {
        if kubeLinked && !isKube && feature == .workloads { return true }
        return allows(feature)
    }
}
