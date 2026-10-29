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

/// EKS's IAM Identity Center fields (Go kube_auth_eks.go).
public let kubeAWSStartURLField = "awsSsoStartUrl"
public let kubeAWSRegionField = "awsSsoRegion"
public let kubeAWSAccountField = "awsAccountId"
public let kubeAWSRoleField = "awsRoleName"
/// EKS discovery's optional regions to read with IAM Identity Center.
public let kubeAWSRegionsField = "awsRegions"
/// EKS discovery's roles to read clusters with: "<accountId>/<roleName>", space separated.
public let kubeAWSRoleSelectionField = "awsRoleSelection"

/// The `kubeAWSRoleSelectionField` value for `choices`, in the order given.
public func kubeRoleSelection(_ choices: [KubeSsoRoleChoice]) -> String {
    choices.map { "\($0.accountId)/\($0.roleName)" }.joined(separator: " ")
}

/// DiscoverSignInChoices' answer: the accounts and roles to pick from, none for "".
public func decodeKubeSsoRoleChoices(_ json: String) -> [KubeSsoRoleChoice] {
    json.isEmpty ? [] : (try? TalosJSON.decode([KubeSsoRoleChoice].self, from: json)) ?? []
}

/// An AWS account and a role IAM Identity Center lets the signed-in user assume.
public struct KubeSsoRoleChoice: Codable, Equatable, Hashable, Sendable {
    public let accountId: String
    public let accountName: String
    public let roleName: String

    public init(accountId: String, accountName: String = "", roleName: String) {
        self.accountId = accountId
        self.accountName = accountName
        self.roleName = roleName
    }

    private enum CodingKeys: String, CodingKey { case accountId, accountName, roleName }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        accountId = try c.field(.accountId, "")
        accountName = try c.field(.accountName, "")
        roleName = try c.field(.roleName, "")
    }

    /// "<account name> · <role>", the account ID when the account has no name.
    public var label: String { "\(accountName.isEmpty ? accountId : accountName) · \(roleName)" }

    /// This account and role are the ones `values` name (an imported ~/.aws/config profile).
    public func matches(_ values: [String: String]) -> Bool {
        accountId == values["awsAccountId"] && roleName == values["awsRoleName"]
    }

    /// The KubeSetCredentials JSON that signs in with this role: the start URL and region of
    /// `values`, this account and role.
    public func secretsJSON(values: [String: String]) -> String {
        var picked = values.filter { $0.key == kubeAWSStartURLField || $0.key == kubeAWSRegionField }
        picked[kubeAWSAccountField] = accountId
        picked[kubeAWSRoleField] = roleName
        return kubeSecretsJSON(
            fields: [kubeAWSStartURLField, kubeAWSRegionField, kubeAWSAccountField, kubeAWSRoleField], values: picked)
    }
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
    /// The accounts and roles an IAM Identity Center sign-in may assume, when several: the user
    /// picks one.
    public let choices: [KubeSsoRoleChoice]

    public init(
        method: String, kind: String, fields: [String] = [], options: [[String]] = [],
        values: [String: String] = [:], signedIn: Bool = false, user: String? = nil, sessionExpires: Int64 = 0,
        choices: [KubeSsoRoleChoice] = []
    ) {
        self.method = method
        self.kind = kind
        self.fields = fields
        self.options = options
        self.values = values
        self.signedIn = signedIn
        self.user = user
        self.sessionExpires = sessionExpires
        self.choices = choices
    }

    private enum CodingKeys: String, CodingKey {
        case method, kind, fields, options, values, signedIn, user, sessionExpires, choices
    }

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
        choices = try c.field(.choices, [])
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

    public init(
        kind: String, url: String, userCode: String? = nil, verificationURL: String? = nil,
        redirectPrefix: String? = nil, expiresIn: Int = 0
    ) {
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
    case "gcpServiceAccountJson", "gcpUserCredentialsJson", kubeLoginConfigField: .json
    case "awsSecretAccessKey", "awsSessionToken", "azureClientSecret", "doApiToken", "rancherApiKey",
        "serviceAccountKey",
        "gcpOAuthClientSecret":
        .secret
    default: .plain
    }
}

/// GKE's "Sign in with Google" option (a build with Ichor's iOS client): a marker, never a
/// text field; its value tells the Go core which platform's sign-in runs.
public let kubeGoogleSignInField = "gcpGoogleSignIn"

/// What the GKE "Sign in with Google" option submits to the Go core (KubeSetCredentials):
/// the browser sign-in then starts.
public func kubeGoogleSignInSecretsJSON(projects: String = "") -> String {
    kubeSecretsJSON(
        fields: [kubeGoogleSignInField, "gcpProjects"],
        values: [kubeGoogleSignInField: "ios", "gcpProjects": projects])
}

/// GKE's "Your Google account (like gcloud)" option: the browser sign-in with gcloud's own
/// client, on every build. A marker drawn as a Sign in button, never a text field; it is
/// submitted as "1".
public let kubeGcloudSignInField = "gcpGcloudSignIn"

/// GKE's workforce identity option: the login config the organisation's administrator hands
/// out; the sign-in then opens the organisation's identity provider in the browser.
public let kubeLoginConfigField = "gcpLoginConfigJson"

/// Discovery's OAuth, gcloud, workforce login config, native iOS Google and IAM Identity
/// Center options need a browser round first.
public func kubeDiscoverNeedsSignIn(_ fields: [String]) -> Bool {
    fields.contains(kubeGoogleSignInField) || fields.contains(kubeGcloudSignInField)
        || fields.contains("gcpOAuthClientId")
        || fields.contains(kubeLoginConfigField) || fields.contains(kubeAWSStartURLField)
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
    // The account and role of IAM Identity Center are picked from a list after the sign-in.
    field == "awsSessionToken" || field == "gcpProjects" || field == "gcpOAuthRedirectUrl"
        || field == kubeAWSAccountField || field == kubeAWSRoleField || field == kubeAWSRegionsField
}

/// The JSON object of `values` for `fields` (trimmed, empty ones left out), as KubeSetCredentials
/// and DiscoverClusters take it; gcloud's sign-in sends its marker.
public func kubeSecretsJSON(fields: [String], values: [String: String]) -> String {
    var secrets: [String: String] = [:]
    for field in fields where field == kubeGcloudSignInField {
        secrets[field] = "1"
    }
    for field in fields where field != kubeGcloudSignInField {
        let value = (values[field] ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        if !value.isEmpty { secrets[field] = value }
    }
    let data = (try? JSONSerialization.data(withJSONObject: secrets, options: [.sortedKeys])) ?? Data("{}".utf8)
    return String(decoding: data, as: UTF8.self)
}

/// Whether every required field of `fields` has a value; a marker needs none.
public func kubeFieldsComplete(_ fields: [String], values: [String: String]) -> Bool {
    fields.allSatisfy {
        $0 == kubeGcloudSignInField || kubeFieldOptional($0)
            || !(values[$0] ?? "").trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }
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
/// many projects, EKS with IAM Identity Center many regions. `projects` is 0 while they are
/// still being listed; `refused` is the AWS regions that would not list clusters; `current` the
/// projects (regions) being read right now; `found` the first clusters found (`clusters` counts
/// them all).
public struct DiscoveryProgress: Decodable, Equatable {
    public var running = false
    public var projects = 0
    public var scanned = 0
    public var clusters = 0
    public var refused: [String] = []
    public var current: [String] = []
    public var found: [String] = []

    public init(
        running: Bool = false, projects: Int = 0, scanned: Int = 0, clusters: Int = 0, refused: [String] = [],
        current: [String] = [], found: [String] = []
    ) {
        self.running = running
        self.projects = projects
        self.scanned = scanned
        self.clusters = clusters
        self.refused = refused
        self.current = current
        self.found = found
    }

    private enum CodingKeys: String, CodingKey { case running, projects, scanned, clusters, refused, current, found }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        running = try c.field(.running, false)
        projects = try c.field(.projects, 0)
        scanned = try c.field(.scanned, 0)
        clusters = try c.field(.clusters, 0)
        refused = try c.field(.refused, [])
        current = try c.field(.current, [])
        found = try c.field(.found, [])
    }

    /// The share of the projects read, nil while they are listed (or none are read).
    public var fraction: Double? {
        projects > 0 ? Double(min(scanned, projects)) / Double(projects) : nil
    }

    /// The clusters counted but not named in `found`.
    public var foundMore: Int { max(clusters - found.count, 0) }

    public static func decode(_ json: String) -> DiscoveryProgress? {
        try? TalosJSON.decode(DiscoveryProgress.self, from: json)
    }
}

/// How far the sign-ins after a cloud import got (Go SignInProgress): `done` of `total` clusters,
/// `current` the one being signed in, `failed` those that could not be.
public struct SignInProgress: Decodable, Equatable {
    public var running = false
    public var done = 0
    public var total = 0
    public var current = ""
    public var failed: [String] = []

    public init(running: Bool = false, done: Int = 0, total: Int = 0, current: String = "", failed: [String] = []) {
        self.running = running
        self.done = done
        self.total = total
        self.current = current
        self.failed = failed
    }

    private enum CodingKeys: String, CodingKey { case running, done, total, current, failed }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        running = try c.field(.running, false)
        done = try c.field(.done, 0)
        total = try c.field(.total, 0)
        current = try c.field(.current, "")
        failed = try c.field(.failed, [])
    }

    /// The share of the clusters signed in (tried), 0 before the first.
    public var fraction: Double {
        total > 0 ? Double(min(done, total)) / Double(total) : 0
    }

    public static func decode(_ json: String) -> SignInProgress? {
        try? TalosJSON.decode(SignInProgress.self, from: json)
    }
}

/// A discovered cluster SignInDiscovered could not sign in, and why.
public struct SignInFailure: Decodable, Equatable {
    public var name: String
    public var error: String

    public init(name: String, error: String) {
        self.name = name
        self.error = error
    }
}

/// SignInDiscovered's answer: the clusters that could not be signed in, as "name: error".
public func decodeSignInDiscovered(_ json: String) throws -> [String] {
    struct Result: Decodable { var failed: [SignInFailure]? }
    return try (TalosJSON.decode(Result.self, from: json).failed ?? []).map { "\($0.name): \($0.error)" }
}

/// The credentials `provider` takes, as field sets: its options, else its one set of fields.
public func kubeDiscoverOptionSets(provider: String, fields: [String: [String]], options: [String: [[String]]])
    -> [[String]]
{
    let sets = (options[provider] ?? []).filter { !$0.isEmpty }
    if !sets.isEmpty { return sets }
    return fields[provider].map { [$0] } ?? []
}

/// The stored names of the contexts an import added that sign in through a method (`signIn`):
/// those the user kept, under the name the core gives a taken one unless it replaced the same
/// cluster. What discovery signs in with the account's credentials once saved.
public func importedSignInContexts(
    summary: ConfigSummary, selected: Set<Int>, replacing: Set<Int>,
    conflicts: [(index: Int, suggested: String)]
) -> [String] {
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
public func kubeAccessContext(of talos: ContextSummary?, links: [String: String], kubeContexts: [ContextSummary])
    -> String?
{
    // An Omni cluster's Kubernetes goes through Omni, with its sign-in: no link applies.
    guard let talos, !talos.isKube, !talos.demo, !talos.omni, !talos.fingerprint.isEmpty,
        let target = links[talos.fingerprint], !target.isEmpty
    else { return nil }
    return kubeContexts.first { $0.isKube && $0.fingerprint == target }?.name
}

/// The links whose Talos cluster and kube cluster are both still stored.
public func keepKubeAccess(_ links: [String: String], talos: [String], kube: [String]) -> [String: String] {
    let talosKnown = Set(talos.filter { !$0.isEmpty })
    let kubeKnown = Set(kube.filter { !$0.isEmpty })
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

/// Routing metadata from Go ClassifyImportText, without any imported secrets.
public struct ImportTextRoute: Decodable, Sendable {
    public let kind: String
    public let provider: String?
    public let field: String
    /// The fields of a secret-free file (an ~/.aws/config: start URL, regions, account, role) to prefill.
    public let values: [String: String]
    public var isGkeCredential: Bool {
        kind == "credentials" && provider == "gke"
            && ["gcpUserCredentialsJson", "gcpServiceAccountJson", kubeLoginConfigField].contains(field)
    }

    /// An ~/.aws/config that signs in with IAM Identity Center: EKS discovery with `values`.
    public var isEksIdentityCenter: Bool {
        kind == "credentials" && provider == "eks" && values["awsSsoStartUrl"] != nil
    }

    /// An ~/.aws/credentials file: access keys are typed on the EKS discovery screen, not scanned.
    public var isAwsAccessKeys: Bool { kind == "refused" && provider == "eks" }

    /// An omnictl config, or a whole Omni account's talosconfig: the Omni sign-in prefilled with
    /// `values` (omniUrl, omniEmail); `field` is serviceAccountKey for a service account identity.
    public var isOmni: Bool { kind == "credentials" && provider == "omni" && values["omniUrl"] != nil }
    public var isOmniServiceAccount: Bool { isOmni && field == "serviceAccountKey" }

    private enum CodingKeys: String, CodingKey { case kind, provider, field, values }
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kind = try c.decode(String.self, forKey: .kind)
        provider = try c.decodeIfPresent(String.self, forKey: .provider)
        field = try c.decodeIfPresent(String.self, forKey: .field) ?? ""
        values = try c.decodeIfPresent([String: String].self, forKey: .values) ?? [:]
    }
}
