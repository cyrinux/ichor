import Foundation

/// What an upgrade of one node would do and what stands in the way (UpgradePlan).
public struct UpgradePlan: Decodable, Equatable, Sendable {
    public struct Etcd: Decodable, Equatable, Sendable {
        public let members: Int
        public let healthy: Int
        public let thisNodeMember: Bool
        /// The cluster keeps quorum while this node is down.
        public let quorumAfterLoss: Bool

        public init(members: Int, healthy: Int, thisNodeMember: Bool, quorumAfterLoss: Bool) {
            self.members = members
            self.healthy = healthy
            self.thisNodeMember = thisNodeMember
            self.quorumAfterLoss = quorumAfterLoss
        }

        private enum CodingKeys: String, CodingKey { case members, healthy, thisNodeMember, quorumAfterLoss }

        public init(from decoder: Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            members = try c.decodeIfPresent(Int.self, forKey: .members) ?? 0
            healthy = try c.decodeIfPresent(Int.self, forKey: .healthy) ?? 0
            thisNodeMember = try c.decodeIfPresent(Bool.self, forKey: .thisNodeMember) ?? false
            quorumAfterLoss = try c.decodeIfPresent(Bool.self, forKey: .quorumAfterLoss) ?? true
        }
    }

    public let node: String
    public let hostname: String
    public let controlPlane: Bool
    public let currentVersion: String
    public let currentImage: String
    /// Image Factory schematic ID, empty for the stock installer.
    public let schematic: String
    /// nil on workers or when etcd could not be read.
    public let etcd: Etcd?
    public let blockers: [String]
    public let warnings: [String]
    /// Every blocker is an etcd check that force skips (nil from an older core: guessed
    /// from the blocker texts).
    public let forceable: Bool?

    public init(node: String, hostname: String = "", controlPlane: Bool = false, currentVersion: String = "",
                currentImage: String = "", schematic: String = "", etcd: Etcd? = nil,
                blockers: [String] = [], warnings: [String] = [], forceable: Bool? = nil) {
        self.node = node
        self.hostname = hostname
        self.controlPlane = controlPlane
        self.currentVersion = currentVersion
        self.currentImage = currentImage
        self.schematic = schematic
        self.etcd = etcd
        self.blockers = blockers
        self.warnings = warnings
        self.forceable = forceable
    }

    private enum CodingKeys: String, CodingKey {
        case node, hostname, controlPlane, currentVersion, currentImage, schematic, etcd, blockers, warnings, forceable
    }

    // Go encodes empty slices as null and may omit empty strings.
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        node = try c.decodeIfPresent(String.self, forKey: .node) ?? ""
        hostname = try c.decodeIfPresent(String.self, forKey: .hostname) ?? ""
        controlPlane = try c.decodeIfPresent(Bool.self, forKey: .controlPlane) ?? false
        currentVersion = try c.decodeIfPresent(String.self, forKey: .currentVersion) ?? ""
        currentImage = try c.decodeIfPresent(String.self, forKey: .currentImage) ?? ""
        schematic = try c.decodeIfPresent(String.self, forKey: .schematic) ?? ""
        etcd = try c.decodeIfPresent(Etcd.self, forKey: .etcd)
        blockers = try c.decodeIfPresent([String].self, forKey: .blockers) ?? []
        warnings = try c.decodeIfPresent([String].self, forKey: .warnings) ?? []
        forceable = try c.decodeIfPresent(Bool.self, forKey: .forceable)
    }
}

/// A Talos release offered as upgrade target (TalosReleases).
public struct TalosRelease: Decodable, Equatable, Identifiable, Sendable {
    public let version: String
    /// As published, may be empty.
    public let date: String
    public let prerelease: Bool

    public var id: String { version }

    public init(version: String, date: String = "", prerelease: Bool = false) {
        self.version = version
        self.date = date
        self.prerelease = prerelease
    }

    private enum CodingKeys: String, CodingKey { case version, date, prerelease }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        version = try c.decode(String.self, forKey: .version)
        date = try c.decodeIfPresent(String.self, forKey: .date) ?? ""
        prerelease = try c.decodeIfPresent(Bool.self, forKey: .prerelease) ?? false
    }
}

/// "1.11.2" -> "v1.11.2"; nil unless it looks like a Talos version (vMAJOR.MINOR.PATCH[-pre]).
public func normalizedTalosVersion(_ text: String) -> String? {
    var version = text.trimmingCharacters(in: .whitespacesAndNewlines)
    if version.hasPrefix("V") { version = "v" + version.dropFirst() }
    if !version.hasPrefix("v") { version = "v" + version }
    let pattern = #"^v\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$"#
    return version.range(of: pattern, options: .regularExpression) != nil ? version : nil
}

/// Numeric (major, minor, patch) of a version, nil when it does not parse.
public func talosVersionParts(_ version: String) -> [Int]? {
    guard let normalized = normalizedTalosVersion(version) else { return nil }
    let core = normalized.dropFirst().split(separator: "-", maxSplits: 1)[0]
    let parts = core.split(separator: ".").compactMap { Int($0) }
    return parts.count == 3 ? parts : nil
}

/// True when `target` is older than `current` (pre-release suffixes ignored).
public func isTalosDowngrade(from current: String, to target: String) -> Bool {
    guard let a = talosVersionParts(current), let b = talosVersionParts(target) else { return false }
    return b.lexicographicallyPrecedes(a)
}

/// Releases to suggest: not the running version, stable ones unless `includePrerelease`,
/// in Go's order (newest first).
public func upgradeSuggestions(_ releases: [TalosRelease], current: String, includePrerelease: Bool) -> [TalosRelease] {
    let running = normalizedTalosVersion(current)
    return releases.filter { ($0.prerelease ? includePrerelease : true) && normalizedTalosVersion($0.version) != running }
}

/// A blocker that `talosctl upgrade --force` skips (the etcd health and quorum checks).
public func isEtcdBlocker(_ text: String) -> Bool {
    let lower = text.lowercased()
    return lower.contains("etcd") || lower.contains("quorum")
}

/// The upgrade form's state for a plan and the user's choices.
public struct UpgradeGate: Equatable, Sendable {
    /// Force is offered only when every blocker is an etcd check that --force skips.
    public let forceAvailable: Bool
    public let canStart: Bool
    /// The typed version does not parse.
    public let invalidVersion: Bool
    /// The target is older than the running version.
    public let downgrade: Bool
    /// Same as the running version (reinstalls it, e.g. to apply a new schematic).
    public let sameVersion: Bool

    public init(plan: UpgradePlan, targetVersion: String, force: Bool, busy: Bool) {
        let target = normalizedTalosVersion(targetVersion)
        forceAvailable = !plan.blockers.isEmpty && (plan.forceable ?? plan.blockers.allSatisfy(isEtcdBlocker))
        invalidVersion = target == nil
        downgrade = target.map { isTalosDowngrade(from: plan.currentVersion, to: $0) } ?? false
        sameVersion = target != nil && target == normalizedTalosVersion(plan.currentVersion)
        let unblocked = plan.blockers.isEmpty || (force && forceAvailable)
        canStart = unblocked && target != nil && !busy
    }
}

public enum UpgradePhase: String, CaseIterable, Comparable, Sendable {
    case requested, installing, rebooting, waiting, booted, done

    /// Go phase name -> phase; "waiting for node" (Go's), "waiting_for_node"… are all `waiting`.
    public init?(go name: String) {
        let key = name.lowercased().filter { $0.isLetter }
        switch key {
        case "requested", "request", "started": self = .requested
        case "installing", "install", "staging", "staged": self = .installing
        case "rebooting", "reboot": self = .rebooting
        case "waiting", "waitingfornode", "waitingnode", "reconnecting": self = .waiting
        case "booted", "boot", "up": self = .booted
        case "done", "complete", "completed", "finished": self = .done
        default: return nil
        }
    }

    public static func < (a: Self, b: Self) -> Bool {
        allCases.firstIndex(of: a)! < allCases.firstIndex(of: b)!
    }

    public var label: String {
        switch self {
        case .requested: "Requested"
        case .installing: "Installing"
        case .rebooting: "Rebooting"
        case .waiting: "Waiting for node"
        case .booted: "Booted"
        case .done: "Done"
        }
    }
}

/// One OnProgress event: `at` is unix ms (0 when Go did not say).
public struct UpgradeProgress: Decodable, Equatable, Sendable {
    public let phase: String
    public let message: String
    public let at: Int64

    public init(phase: String, message: String = "", at: Int64 = 0) {
        self.phase = phase
        self.message = message
        self.at = at
    }

    private enum CodingKeys: String, CodingKey { case phase, message, at }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        phase = try c.decodeIfPresent(String.self, forKey: .phase) ?? ""
        message = try c.decodeIfPresent(String.self, forKey: .message) ?? ""
        if let ms = try? c.decodeIfPresent(Int64.self, forKey: .at) {
            at = ms
        } else if let text = try? c.decodeIfPresent(String.self, forKey: .at), let date = ISO8601DateFormatter().date(from: text) {
            at = Int64(date.timeIntervalSince1970 * 1000)
        } else {
            at = 0
        }
    }
}

public enum UpgradeStepState: Equatable, Sendable {
    case pending, current, done, failed
}

/// A row of the progress timeline.
public struct UpgradeStep: Equatable, Identifiable, Sendable {
    public let phase: UpgradePhase
    public let state: UpgradeStepState
    /// Unix ms the phase was first reported, 0 when not reached.
    public let at: Int64
    /// Latest message of the phase.
    public let message: String

    public var id: UpgradePhase { phase }

    public init(phase: UpgradePhase, state: UpgradeStepState, at: Int64 = 0, message: String = "") {
        self.phase = phase
        self.state = state
        self.at = at
        self.message = message
    }
}

/// Timeline of all phases from the events received so far. Phases only move forward: a late
/// event of an earlier phase only updates its message. `failure` marks the furthest phase
/// (or "requested" when nothing was reported) failed, with the failure as its message.
public func upgradeTimeline(_ events: [UpgradeProgress], finished: Bool = false, failure: String? = nil) -> [UpgradeStep] {
    var first: [UpgradePhase: Int64] = [:]
    var messages: [UpgradePhase: String] = [:]
    var furthest: UpgradePhase?
    for event in events {
        guard let phase = UpgradePhase(go: event.phase) else {
            // Unknown phase name: its message goes to the current step.
            if let furthest, !event.message.isEmpty { messages[furthest] = event.message }
            continue
        }
        if first[phase] == nil { first[phase] = event.at }
        if !event.message.isEmpty { messages[phase] = event.message }
        if furthest.map({ phase > $0 }) ?? true { furthest = phase }
    }
    if let failure {
        let failed = furthest ?? .requested
        return UpgradePhase.allCases.map { phase in
            if phase < failed { return UpgradeStep(phase: phase, state: .done, at: first[phase] ?? 0, message: messages[phase] ?? "") }
            if phase == failed { return UpgradeStep(phase: phase, state: .failed, at: first[phase] ?? 0, message: failure) }
            return UpgradeStep(phase: phase, state: .pending)
        }
    }
    let reached = finished ? UpgradePhase.done : furthest
    return UpgradePhase.allCases.map { phase in
        let state: UpgradeStepState
        if let reached, phase < reached || (phase == reached && (phase == .done || finished)) {
            state = .done
        } else if phase == reached {
            state = .current
        } else {
            state = .pending
        }
        return UpgradeStep(phase: phase, state: state, at: first[phase] ?? 0, message: messages[phase] ?? "")
    }
}

/// TalosUpdateCheck's answer: the latest stable Talos and how many nodes run an older one.
public struct TalosUpdateInfo: Decodable, Equatable, Sendable {
    public let latest: String
    public let latestDate: String
    /// Some node runs an older version than `latest`.
    public let newer: Bool
    /// Nodes on older versions.
    public let outdated: Int
    /// Oldest version running.
    public let oldest: String
    /// Release notes URL.
    public let notes: String

    public init(latest: String, latestDate: String = "", newer: Bool, outdated: Int = 0, oldest: String = "", notes: String = "") {
        self.latest = latest
        self.latestDate = latestDate
        self.newer = newer
        self.outdated = outdated
        self.oldest = oldest
        self.notes = notes
    }

    private enum CodingKeys: String, CodingKey { case latest, latestDate, newer, outdated, oldest, notes }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        latest = try c.decodeIfPresent(String.self, forKey: .latest) ?? ""
        latestDate = try c.decodeIfPresent(String.self, forKey: .latestDate) ?? ""
        newer = try c.decodeIfPresent(Bool.self, forKey: .newer) ?? false
        // A count, or the list of outdated entries.
        if let count = try? c.decodeIfPresent(Int.self, forKey: .outdated) {
            outdated = count
        } else if let list = try? c.decodeIfPresent([String].self, forKey: .outdated) {
            outdated = list.count
        } else {
            outdated = 0
        }
        oldest = try c.decodeIfPresent(String.self, forKey: .oldest) ?? ""
        notes = try c.decodeIfPresent(String.self, forKey: .notes) ?? ""
    }
}

/// Minimum time between two update checks.
public let talosUpdateCheckInterval: TimeInterval = 6 * 3_600

public func shouldCheckTalosUpdate(lastCheck: Date?, now: Date = Date()) -> Bool {
    guard let lastCheck else { return true }
    return now.timeIntervalSince(lastCheck) >= talosUpdateCheckInterval || now < lastCheck
}

/// A node version older than `latest` (unknown versions are not counted).
public func isOutdatedTalos(_ version: String, latest: String) -> Bool {
    isTalosDowngrade(from: latest, to: version)
}

/// TalosUpdateCheck's input: one known version per node (duplicates kept, Go counts the
/// outdated entries), sorted so the same cluster gives the same string, comma-separated.
public func talosVersionsCSV(_ versions: [String]) -> String {
    versions.filter { !$0.isEmpty }.sorted().joined(separator: ",")
}

/// Nodes on the banner: count from Go, else the local one; nil when there is nothing to show.
public func talosUpdateBannerCount(_ info: TalosUpdateInfo, localOutdated: Int) -> Int? {
    guard info.newer, !info.latest.isEmpty else { return nil }
    return info.outdated > 0 ? info.outdated : localOutdated
}
