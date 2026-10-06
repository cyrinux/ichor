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
            members = try c.field(.members, 0)
            healthy = try c.field(.healthy, 0)
            thisNodeMember = try c.field(.thisNodeMember, false)
            quorumAfterLoss = try c.field(.quorumAfterLoss, true)
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
    /// Risks the user confirms one by one before starting (StartUpgrade refuses them
    /// unacknowledged; force does not skip them).
    public let acknowledge: [String]
    /// Every blocker is an etcd check that force skips (nil from an older core: guessed
    /// from the blocker texts).
    public let forceable: Bool?

    public init(node: String, hostname: String = "", controlPlane: Bool = false, currentVersion: String = "",
                currentImage: String = "", schematic: String = "", etcd: Etcd? = nil,
                blockers: [String] = [], warnings: [String] = [], acknowledge: [String] = [], forceable: Bool? = nil) {
        self.node = node
        self.hostname = hostname
        self.controlPlane = controlPlane
        self.currentVersion = currentVersion
        self.currentImage = currentImage
        self.schematic = schematic
        self.etcd = etcd
        self.blockers = blockers
        self.warnings = warnings
        self.acknowledge = acknowledge
        self.forceable = forceable
    }

    private enum CodingKeys: String, CodingKey {
        case node, hostname, controlPlane, currentVersion, currentImage, schematic, etcd, blockers, warnings, acknowledge, forceable
    }

    // Go encodes empty slices as null and may omit empty strings.
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        node = try c.field(.node, "")
        hostname = try c.field(.hostname, "")
        controlPlane = try c.field(.controlPlane, false)
        currentVersion = try c.field(.currentVersion, "")
        currentImage = try c.field(.currentImage, "")
        schematic = try c.field(.schematic, "")
        etcd = try c.decodeIfPresent(Etcd.self, forKey: .etcd)
        blockers = try c.field(.blockers, [])
        warnings = try c.field(.warnings, [])
        acknowledge = try c.field(.acknowledge, [])
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
        date = try c.field(.date, "")
        prerelease = try c.field(.prerelease, false)
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
    /// The plan's risks to acknowledge, then the target version's (UpgradeVersionCheck).
    public let acknowledgments: [String]
    /// The form can ask for confirmation: everything but the acknowledgments, which the
    /// confirmation collects.
    public let canRequest: Bool
    /// canRequest, and the acknowledgments (if any) are confirmed.
    public let canStart: Bool
    /// The typed version does not parse.
    public let invalidVersion: Bool
    /// The target is older than the running version.
    public let downgrade: Bool
    /// Same as the running version (reinstalls it, e.g. to apply a new schematic).
    public let sameVersion: Bool

    /// `versionRisk` is UpgradeVersionCheck's answer for the target ("" for none).
    public init(plan: UpgradePlan, targetVersion: String, versionRisk: String = "", force: Bool, busy: Bool,
                acknowledged: Bool = false) {
        let target = normalizedTalosVersion(targetVersion)
        forceAvailable = !plan.blockers.isEmpty && (plan.forceable ?? plan.blockers.allSatisfy(isEtcdBlocker))
        invalidVersion = target == nil
        downgrade = target.map { isTalosDowngrade(from: plan.currentVersion, to: $0) } ?? false
        sameVersion = target != nil && target == normalizedTalosVersion(plan.currentVersion)
        let risk = versionRisk.trimmingCharacters(in: .whitespacesAndNewlines)
        acknowledgments = plan.acknowledge + (risk.isEmpty ? [] : [risk])
        let unblocked = plan.blockers.isEmpty || (force && forceAvailable)
        canRequest = unblocked && target != nil && !busy
        canStart = canRequest && (acknowledgments.isEmpty || acknowledged)
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

    /// Until the node reboots the app may drive the upgrade (on Talos 1.18+ it pulls and
    /// installs through the app's connection): suspending the app stalls it.
    public var needsApp: Bool { self < .rebooting }

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
        phase = try c.field(.phase, "")
        message = try c.field(.message, "")
        if let ms = try? c.decodeIfPresent(Int64.self, forKey: .at) {
            at = ms
        } else if let text = try? c.decodeIfPresent(String.self, forKey: .at), let date = ISO8601DateFormatter().date(from: text) {
            at = date.epochMillis
        } else {
            at = 0
        }
    }
}

/// A row of the progress timeline.
public typealias UpgradeStep = TimelineStep<UpgradePhase>

/// Timeline of all phases from the events received so far. Phases only move forward: a late
/// event of an earlier phase only updates its message. `failure` marks the furthest phase
/// (or "requested" when nothing was reported) failed, with the failure as its message.
public func upgradeTimeline(_ events: [UpgradeProgress], finished: Bool = false, failure: String? = nil) -> [UpgradeStep] {
    // An unknown phase name lends its message to the current step.
    foldTimeline(events, phases: UpgradePhase.allCases, finished: finished, failure: failure, terminal: .done,
                 strayMessagesToCurrent: true, phase: { UpgradePhase(go: $0.phase) }, at: \.at, message: \.message)
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
        latest = try c.field(.latest, "")
        latestDate = try c.field(.latestDate, "")
        newer = try c.field(.newer, false)
        // A count, or the list of outdated entries.
        if let count = try? c.decodeIfPresent(Int.self, forKey: .outdated) {
            outdated = count
        } else if let list = try? c.decodeIfPresent([String].self, forKey: .outdated) {
            outdated = list.count
        } else {
            outdated = 0
        }
        oldest = try c.field(.oldest, "")
        notes = try c.field(.notes, "")
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

/// The upgrade chooser's nodes by role, in the order to upgrade them: control plane first.
public struct TalosUpgradeChoices: Equatable, Sendable {
    public let controlPlane: [NodeOverview]
    public let workers: [NodeOverview]

    public var count: Int { controlPlane.count + workers.count }
    /// The workers are better left for later: a control-plane node is still on an older version.
    public var workersWait: Bool { !controlPlane.isEmpty }
}

/// Reachable nodes older than `latest`, oldest first then by hostname, split by role; a node
/// that is not control plane counts as a worker.
public func talosUpgradeChoices(_ nodes: [NodeOverview], latest: String) -> TalosUpgradeChoices {
    let outdated = nodes.filter { $0.reachable && isOutdatedTalos($0.version, latest: latest) }
        .sorted { a, b in
            if a.version != b.version { return isTalosDowngrade(from: b.version, to: a.version) }
            return a.hostname < b.hostname
        }
    return TalosUpgradeChoices(
        controlPlane: outdated.filter { $0.role == "controlplane" },
        workers: outdated.filter { $0.role != "controlplane" }
    )
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
