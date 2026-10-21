import Foundation

/// What an app backup holds once the Go core opened it (IchorgoDecryptBackup): the
/// talosconfig and the settings that go with it. Same JSON as the Android app's, so a backup
/// moves between platforms; a setting this app does not have is ignored, one the backup lacks
/// (nil) keeps its current value.
///
/// Format 3 adds the pins, the Git hosts, the AI diagnosis settings and, only when the user
/// opts in, their API keys and Git tokens (`secrets`). Not included: the app lock's secret
/// and security-key data (device-bound; only whether a lock was on), the audit log, caches.
public struct BackupPayload: Codable, Equatable, Sendable {
    /// The payload version, checked by the Go core; bump it for changes older apps cannot read.
    public var format: Int
    public var platform: String?
    /// Unix seconds.
    public var createdAt: Int64?
    /// "" when only clusters added from a kubeconfig are stored (format 2).
    public var talosconfig: String
    /// The stored kubeconfig (clusters added without Talos), format 2 only; nil when there is none.
    public var kubeconfig: String?
    /// The cluster on screen, by position (Talos clusters, then kubeconfig ones): the
    /// screenshot mode masks context names.
    public var activeContextIndex: Int?
    public var settings: BackupSettings?
    /// Per-cluster options by context fingerprint, which both apps compute alike in the Go core.
    public var clusters: [String: BackupCluster]?
    /// The sign-ins of the kubeconfig clusters by fingerprint (Go KubeAuthForBackup: what the
    /// user entered, never a session); nil when there is none. Older apps ignore it.
    public var kubeAuth: [String: String]?
    /// The real-scope pins by context fingerprint (format 3); a pin this version cannot read is skipped.
    public var pins: [String: [Pin]]?
    /// The Git hosts, without their tokens (format 3).
    public var gitHosts: [GitHost]?
    /// The AI diagnosis settings, without their keys (format 3).
    public var ai: BackupAI?
    /// The AI keys and Git tokens: only when the user opted in, else left out of the JSON.
    public var secrets: BackupSecrets?

    public init(
        format: Int = backupFormat, platform: String? = nil, createdAt: Int64? = nil, talosconfig: String,
        kubeconfig: String? = nil, activeContextIndex: Int? = nil, settings: BackupSettings? = nil,
        clusters: [String: BackupCluster]? = nil, kubeAuth: [String: String]? = nil,
        pins: [String: [Pin]]? = nil, gitHosts: [GitHost]? = nil, ai: BackupAI? = nil, secrets: BackupSecrets? = nil
    ) {
        self.format = format
        self.platform = platform
        self.createdAt = createdAt
        self.talosconfig = talosconfig
        self.kubeconfig = kubeconfig
        self.activeContextIndex = activeContextIndex
        self.settings = settings
        self.clusters = clusters
        self.kubeAuth = kubeAuth
        self.pins = pins
        self.gitHosts = gitHosts
        self.ai = ai
        self.secrets = secrets
    }

    private enum CodingKeys: String, CodingKey {
        case format, platform, createdAt, talosconfig, kubeconfig, activeContextIndex, settings, clusters, kubeAuth
        case pins, gitHosts, ai, secrets
    }

    // A format 2 payload may leave the talosconfig out (or null) when it only holds a kubeconfig.
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        format = try c.decode(Int.self, forKey: .format)
        platform = try c.decodeIfPresent(String.self, forKey: .platform)
        createdAt = try c.decodeIfPresent(Int64.self, forKey: .createdAt)
        talosconfig = try c.field(.talosconfig, "")
        kubeconfig = try c.decodeIfPresent(String.self, forKey: .kubeconfig)
        activeContextIndex = try c.decodeIfPresent(Int.self, forKey: .activeContextIndex)
        settings = try c.decodeIfPresent(BackupSettings.self, forKey: .settings)
        clusters = try c.decodeIfPresent([String: BackupCluster].self, forKey: .clusters)
        kubeAuth = try c.decodeIfPresent([String: String].self, forKey: .kubeAuth)
        pins = try c.decodeIfPresent([String: [LenientBackupPin]].self, forKey: .pins)?
            .mapValues { $0.compactMap(\.pin) }
        gitHosts = try c.decodeIfPresent([GitHost].self, forKey: .gitHosts)?.filter { !$0.host.isEmpty }
        ai = try c.decodeIfPresent(BackupAI.self, forKey: .ai)
        secrets = try c.decodeIfPresent(BackupSecrets.self, forKey: .secrets)
    }
}

/// A pin of a backup, nil when this version cannot read it (a newer target).
private struct LenientBackupPin: Decodable {
    let pin: Pin?
    init(from decoder: Decoder) throws { pin = try? Pin(from: decoder) }
}

public struct BackupSettings: Codable, Equatable, Sendable {
    /// "auto", "light", "dark" or "black".
    public var themeMode: String?
    /// Android only (iOS takes the language from the Settings app).
    public var language: String?
    /// Live CPU and memory on the overview.
    public var liveClusterStats: Bool?
    public var privacyMask: Bool?
    public var privacyMaskWords: String?
    public var monitorAlerts: Bool?
    /// Android only (iOS schedules background checks itself).
    public var monitorIntervalMinutes: Int?
    /// Download icons the app does not bundle (Apps inventory).
    public var remoteAppIcons: Bool?
    /// Android only ("Allow screenshots"; iOS cannot block them).
    public var allowScreenshots: Bool?
    /// Whether an app lock was on: "off", "biometric" or "securityKey" (`BackupLockMode`);
    /// never its secret. A restore only offers to set the lock up again.
    public var appLockMode: String?
    /// The monitor's storage thresholds (% used).
    public var storageWarnPercent: Int?
    public var storageCriticalPercent: Int?

    public init(
        themeMode: String? = nil, privacyMask: Bool? = nil, privacyMaskWords: String? = nil, monitorAlerts: Bool? = nil,
        remoteAppIcons: Bool? = nil
    ) {
        self.themeMode = themeMode
        self.privacyMask = privacyMask
        self.privacyMaskWords = privacyMaskWords
        self.monitorAlerts = monitorAlerts
        self.remoteAppIcons = remoteAppIcons
    }
}

public struct BackupCluster: Codable, Equatable, Sendable {
    public var name: String?
    /// 0xRRGGBB.
    public var color: Int?
    /// Reached over a VPN only (nil: no).
    public var vpnOnly: Bool?
    /// How to wake each node, by node address as the talosconfig names it.
    public var wakeOnLan: [String: BackupWolTarget]?
    /// The Kubernetes API address to use instead of the kubeconfig's.
    public var kubeServer: String?
    /// A Talos cluster's Kubernetes access: the fingerprint of the kubeconfig cluster its
    /// Kubernetes calls go through (nil: the admin kubeconfig Talos issues).
    public var kubeAccess: String?
    /// The cluster's own Git repository (its Cluster repository setting); never a token.
    public var repository: ClusterRepo?

    public init(name: String? = nil, color: Int? = nil, vpnOnly: Bool? = nil,
                wakeOnLan: [String: BackupWolTarget]? = nil, kubeServer: String? = nil, kubeAccess: String? = nil,
                repository: ClusterRepo? = nil) {
        self.name = name
        self.color = color
        self.vpnOnly = vpnOnly
        self.wakeOnLan = wakeOnLan
        self.kubeServer = kubeServer
        self.kubeAccess = kubeAccess
        self.repository = repository
    }
}

/// A node's Wake-on-LAN setting in a backup (Android's BackupWolTarget).
public struct BackupWolTarget: Codable, Equatable, Sendable {
    public var mac: String
    public var broadcast: String?
    public var port: Int?

    public init(mac: String, broadcast: String? = nil, port: Int? = nil) {
        self.mac = mac
        self.broadcast = broadcast
        self.port = port
    }
}

public let backupFormat = 1

/// The format of a payload holding a kubeconfig: older apps refuse it ("update the app")
/// instead of restoring it without its kubeconfig clusters.
public let backupFormatKubeconfig = 2

/// The format of a payload holding pins, Git hosts, AI settings and maybe secrets: older apps
/// refuse it ("update the app") instead of silently dropping them.
public let backupFormatSettings = 3

/// The format to write: always 3 (the payload always holds the format 3 settings). Same as Android.
public func backupPayloadFormat() -> Int { backupFormatSettings }

/// The kubeconfig of a restored payload: only a format 2+ one carries it, nil when it holds none.
public func restoredKubeconfig(_ payload: BackupPayload) -> String? {
    guard payload.format >= backupFormatKubeconfig else { return nil }
    let kube = payload.kubeconfig ?? ""
    return kube.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ? nil : kube
}

/// The talosconfig of a restored payload, nil when it holds none (a format 2 kubeconfig-only one).
public func restoredTalosconfig(_ payload: BackupPayload) -> String? {
    payload.talosconfig.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ? nil : payload.talosconfig
}

/// The shortest passphrase the Go core accepts (BackupMinPassphrase), in characters.
public let backupMinPassphrase = 12

/// The extension backups are saved with, on both platforms.
public let backupExtension = "ichorbackup"

/// The file name a new backup is offered under, e.g. "ichor-2026-10-02.ichorbackup"
/// ("ichor-2026-10-02.age" for an age file).
public func backupFileName(date: Date, format: BackupFileFormat = .ichor, calendar: Calendar = .current) -> String {
    let c = calendar.dateComponents([.year, .month, .day], from: date)
    return String(format: "ichor-%04d-%02d-%02d.%@", c.year ?? 0, c.month ?? 0, c.day ?? 0, format.fileExtension)
}

/// Why a new backup's passphrase cannot be used yet.
public enum BackupPassphraseProblem: Equatable, Sendable {
    case tooShort, mismatch
}

public func backupPassphraseProblem(_ passphrase: String, again: String) -> BackupPassphraseProblem? {
    if passphrase.count < backupMinPassphrase { return .tooShort }
    if passphrase != again { return .mismatch }
    return nil
}

/// The per-cluster settings of this device, as a backup stores them (only clusters still in `fingerprints`).
/// `wakeOnLan` is keyed by `wolKey`.
public func backupClusters(fingerprints: [String], names: [String: String], colors: [String: Int],
                           kubeServers: [String: String], vpnOnly: Set<String> = [],
                           wakeOnLan: [String: WolTarget] = [:], kubeAccess: [String: String] = [:],
                           repos: [String: ClusterRepo] = [:]) -> [String: BackupCluster] {
    var out: [String: BackupCluster] = [:]
    for fp in fingerprints where !fp.isEmpty {
        var wol: [String: BackupWolTarget] = [:]
        for (key, target) in wakeOnLan where key.hasPrefix(fp + "|") {
            wol[String(key.dropFirst(fp.count + 1))] = BackupWolTarget(mac: target.mac, broadcast: target.broadcast, port: target.port)
        }
        out[fp] = BackupCluster(name: names[fp], color: colors[fp].map { $0 & 0xFFFFFF },
                                vpnOnly: vpnOnly.contains(fp) ? true : nil, wakeOnLan: wol.isEmpty ? nil : wol,
                                kubeServer: kubeServers[fp], kubeAccess: kubeAccess[fp].flatMap { $0.isEmpty ? nil : $0 },
                                repository: repos[fp])
    }
    return out
}

/// The per-cluster settings to restore, narrowed to `fingerprints` (the restored config's) and checked.
public struct RestoredClusters: Equatable, Sendable {
    public let names: [String: String]
    public let colors: [String: Int]
    /// Trimmed; the Go core checks them before they are stored.
    public let kubeServers: [String: String]
    public var vpnOnly: Set<String> = []
    /// By `wolKey`, checked like typed ones.
    public var wakeOnLan: [String: WolTarget] = [:]
    /// Talos fingerprint → kubeconfig cluster fingerprint, both in the restored config.
    public var kubeAccess: [String: String] = [:]
    /// Normalized; one without an address is dropped.
    public var repos: [String: ClusterRepo] = [:]
}

public func restoredClusters(_ clusters: [String: BackupCluster]?, fingerprints: [String]) -> RestoredClusters {
    let known = Set(fingerprints.filter { !$0.isEmpty })
    var names: [String: String] = [:]
    var colors: [String: Int] = [:]
    var kubeServers: [String: String] = [:]
    var vpnOnly: Set<String> = []
    var wakeOnLan: [String: WolTarget] = [:]
    var kubeAccess: [String: String] = [:]
    var repos: [String: ClusterRepo] = [:]
    for (fp, cluster) in clusters ?? [:] where known.contains(fp) {
        if let repo = cluster.repository.flatMap(normalizeClusterRepo) { repos[fp] = repo }
        if let target = cluster.kubeAccess, target != fp, known.contains(target) { kubeAccess[fp] = target }
        if cluster.vpnOnly == true { vpnOnly.insert(fp) }
        for (node, wol) in cluster.wakeOnLan ?? [:] {
            let node = node.trimmingCharacters(in: .whitespaces)
            let broadcast = (wol.broadcast ?? "").trimmingCharacters(in: .whitespaces)
            guard !node.isEmpty, !broadcast.contains("|"), isWolAddress(broadcast),
                  let target = decodeWolTarget(encodeWolTarget(WolTarget(mac: wol.mac, broadcast: broadcast, port: wol.port ?? wolDefaultPort)))
            else { continue }
            wakeOnLan[wolKey(fingerprint: fp, node: node)] = target
        }
        if let name = cluster.name.flatMap(normalizeClusterName) { names[fp] = name }
        // Android writes RGB too, but drop any alpha a future writer might add.
        if let color = cluster.color { colors[fp] = color & 0xFFFFFF }
        if let server = cluster.kubeServer?.trimmingCharacters(in: .whitespacesAndNewlines), !server.isEmpty {
            kubeServers[fp] = server
        }
    }
    return RestoredClusters(names: names, colors: colors, kubeServers: kubeServers, vpnOnly: vpnOnly,
                            wakeOnLan: wakeOnLan, kubeAccess: kubeAccess, repos: repos)
}

/// The sign-ins of a restored payload for the clusters it restored (`fingerprints`), empty
/// states left out.
public func restoredKubeAuth(_ payload: BackupPayload, fingerprints: [String]) -> [String: String] {
    KubeAuthMap.keeping(payload.kubeAuth ?? [:], fingerprints: fingerprints)
}

/// A backup error of the Go core, whose messages start with a code (backup.go).
public enum BackupError: Error, Equatable, Sendable {
    case passphraseShort, wrongPassphrase, notBackup, unsupported, invalidContent
    /// An age file sealed for public keys only: it opens with the matching age key on a computer.
    case needsIdentity

    public init?(coreMessage: String) {
        let codes: [(String, BackupError)] = [
            ("backup-passphrase-short", .passphraseShort),
            ("backup-wrong-passphrase", .wrongPassphrase),
            ("backup-not-a-backup", .notBackup),
            ("backup-unsupported", .unsupported),
            ("backup-invalid-content", .invalidContent),
            ("backup-needs-identity", .needsIdentity),
        ]
        guard let match = codes.first(where: { coreMessage.hasPrefix($0.0) }) else { return nil }
        self = match.1
    }
}
