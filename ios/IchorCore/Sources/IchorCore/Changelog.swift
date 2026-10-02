import Foundation

/// A group of notes of a release (scripts/changelog.py): what is new, fixed, faster or breaking.
public struct ChangelogSection: Decodable, Equatable, Sendable {
    /// new | fixed | faster | breaking, or a kind this build does not know yet.
    public let kind: String
    /// English heading, used for an unknown kind.
    public let title: String
    public let items: [String]

    public init(kind: String, title: String = "", items: [String] = []) {
        self.kind = kind
        self.title = title
        self.items = items
    }

    private enum CodingKeys: String, CodingKey { case kind, title, items }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kind = try c.decodeIfPresent(String.self, forKey: .kind) ?? ""
        title = try c.decodeIfPresent(String.self, forKey: .title) ?? ""
        items = try c.decodeIfPresent([String].self, forKey: .items) ?? []
    }
}

public struct ChangelogRelease: Decodable, Equatable, Identifiable, Sendable {
    public let version: String
    /// The commit count at the release: the app's build number (CFBundleVersion).
    public let buildNumber: Int
    /// ISO 8601, "" when unknown.
    public let publishedAt: String
    /// Empty for a release without user-facing changes.
    public let sections: [ChangelogSection]

    public var id: String { "\(version)#\(buildNumber)" }

    public init(version: String, buildNumber: Int, publishedAt: String = "", sections: [ChangelogSection] = []) {
        self.version = version
        self.buildNumber = buildNumber
        self.publishedAt = publishedAt
        self.sections = sections
    }

    private enum CodingKeys: String, CodingKey {
        case version, sections
        case buildNumber = "build_number"
        case publishedAt = "published_at"
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        version = try c.decodeIfPresent(String.self, forKey: .version) ?? ""
        buildNumber = try c.decodeIfPresent(Int.self, forKey: .buildNumber) ?? 0
        publishedAt = try c.decodeIfPresent(String.self, forKey: .publishedAt) ?? ""
        // Sections without items say nothing.
        sections = (try c.decodeIfPresent([ChangelogSection].self, forKey: .sections) ?? []).filter { !$0.items.isEmpty }
    }

    /// The release day, nil when the date is missing or not ISO 8601.
    public var published: Date? { ISO8601DateFormatter().date(from: publishedAt) }
}

/// The bundled release history, newest first.
public struct Changelog: Decodable, Equatable, Sendable {
    public let releases: [ChangelogRelease]

    public init(releases: [ChangelogRelease] = []) { self.releases = releases }

    private enum CodingKeys: String, CodingKey { case releases }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        releases = try c.decodeIfPresent([ChangelogRelease].self, forKey: .releases) ?? []
    }
}

/// The history in `data`; empty when the file is missing (nil) or corrupt, so nothing shows.
public func decodeChangelog(_ data: Data?) -> Changelog {
    guard let data, let decoded = try? JSONDecoder().decode(Changelog.self, from: data) else { return Changelog() }
    return decoded
}

/// The section kinds the app has a localized heading for; any other kind shows its JSON title.
public enum ChangelogKind: String, CaseIterable, Sendable {
    case new, fixed, faster, breaking

    /// English heading (the app localizes it).
    public var title: String {
        switch self {
        case .new: "New"
        case .fixed: "Fixed"
        case .faster: "Faster"
        case .breaking: "Breaking changes"
        }
    }
}

public extension ChangelogSection {
    var knownKind: ChangelogKind? { ChangelogKind(rawValue: kind.lowercased()) }

    /// The known kind's heading, else the JSON title, else the raw kind.
    var englishTitle: String {
        if let knownKind { return knownKind.title }
        return title.isEmpty ? kind : title
    }
}

/// The releases newer than `build`, newest first.
public func releasesSince(_ releases: [ChangelogRelease], build: Int) -> [ChangelogRelease] {
    releases.filter { $0.buildNumber > build }.sorted { $0.buildNumber > $1.buildNumber }
}

/// What to show after a launch: the notes of every bundled release newer than the build
/// launched last time. Nothing (empty) on a first install (no previous build), when the
/// build is the same or older (a downgrade), or when the bundled history has nothing newer
/// (also a missing or corrupt file). Same rule as Android.
public func whatsNewReleases(previousBuild: Int?, currentBuild: Int, releases: [ChangelogRelease]) -> [ChangelogRelease] {
    guard let previousBuild, currentBuild > previousBuild else { return [] }
    return releasesSince(releases, build: previousBuild)
}
