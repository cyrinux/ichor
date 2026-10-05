import Foundation

/// What an app backup holds once the Go core opened it (IchorgoDecryptBackup): the
/// talosconfig and the settings that go with it. Same JSON as the Android app's, so a backup
/// moves between platforms; a setting this app does not have is ignored, one the backup lacks
/// (nil) keeps its current value.
///
/// Not included: AI diagnosis settings and keys (a third-party service, enabled per device),
/// the app lock (per-device security), and caches.
public struct BackupPayload: Codable, Equatable, Sendable {
    /// The payload version, checked by the Go core; bump it for changes older apps cannot read.
    public var format: Int
    public var platform: String?
    /// Unix seconds.
    public var createdAt: Int64?
    public var talosconfig: String
    /// The cluster on screen, by position: the screenshot mode masks context names.
    public var activeContextIndex: Int?
    public var settings: BackupSettings?
    /// Per-cluster options by context fingerprint, which both apps compute alike in the Go core.
    public var clusters: [String: BackupCluster]?

    public init(
        format: Int = backupFormat, platform: String? = nil, createdAt: Int64? = nil, talosconfig: String,
        activeContextIndex: Int? = nil, settings: BackupSettings? = nil, clusters: [String: BackupCluster]? = nil
    ) {
        self.format = format
        self.platform = platform
        self.createdAt = createdAt
        self.talosconfig = talosconfig
        self.activeContextIndex = activeContextIndex
        self.settings = settings
        self.clusters = clusters
    }
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

    public init(name: String? = nil, color: Int? = nil, vpnOnly: Bool? = nil,
                wakeOnLan: [String: BackupWolTarget]? = nil, kubeServer: String? = nil) {
        self.name = name
        self.color = color
        self.vpnOnly = vpnOnly
        self.wakeOnLan = wakeOnLan
        self.kubeServer = kubeServer
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

/// The shortest passphrase the Go core accepts (BackupMinPassphrase), in characters.
public let backupMinPassphrase = 12

/// The extension backups are saved with, on both platforms.
public let backupExtension = "ichorbackup"

/// The file name a new backup is offered under, e.g. "ichor-2026-10-02.ichorbackup".
public func backupFileName(date: Date, calendar: Calendar = .current) -> String {
    let c = calendar.dateComponents([.year, .month, .day], from: date)
    return String(format: "ichor-%04d-%02d-%02d.%@", c.year ?? 0, c.month ?? 0, c.day ?? 0, backupExtension)
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
                           wakeOnLan: [String: WolTarget] = [:]) -> [String: BackupCluster] {
    var out: [String: BackupCluster] = [:]
    for fp in fingerprints where !fp.isEmpty {
        var wol: [String: BackupWolTarget] = [:]
        for (key, target) in wakeOnLan where key.hasPrefix(fp + "|") {
            wol[String(key.dropFirst(fp.count + 1))] = BackupWolTarget(mac: target.mac, broadcast: target.broadcast, port: target.port)
        }
        out[fp] = BackupCluster(name: names[fp], color: colors[fp].map { $0 & 0xFFFFFF },
                                vpnOnly: vpnOnly.contains(fp) ? true : nil, wakeOnLan: wol.isEmpty ? nil : wol,
                                kubeServer: kubeServers[fp])
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
}

public func restoredClusters(_ clusters: [String: BackupCluster]?, fingerprints: [String]) -> RestoredClusters {
    let known = Set(fingerprints.filter { !$0.isEmpty })
    var names: [String: String] = [:]
    var colors: [String: Int] = [:]
    var kubeServers: [String: String] = [:]
    var vpnOnly: Set<String> = []
    var wakeOnLan: [String: WolTarget] = [:]
    for (fp, cluster) in clusters ?? [:] where known.contains(fp) {
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
    return RestoredClusters(names: names, colors: colors, kubeServers: kubeServers, vpnOnly: vpnOnly, wakeOnLan: wakeOnLan)
}

/// A backup error of the Go core, whose messages start with a code (backup.go).
public enum BackupError: Error, Equatable, Sendable {
    case passphraseShort, wrongPassphrase, notBackup, unsupported, invalidContent

    public init?(coreMessage: String) {
        let codes: [(String, BackupError)] = [
            ("backup-passphrase-short", .passphraseShort),
            ("backup-wrong-passphrase", .wrongPassphrase),
            ("backup-not-a-backup", .notBackup),
            ("backup-unsupported", .unsupported),
            ("backup-invalid-content", .invalidContent),
        ]
        guard let match = codes.first(where: { coreMessage.hasPrefix($0.0) }) else { return nil }
        self = match.1
    }
}
