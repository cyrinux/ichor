import Foundation

/// A screen of the active cluster as the Go core's SearchScreens ranks it (U3 step 5): its
/// title already localised by the app, which keeps the catalog (ScreenCatalog.swift).
public struct ScreenEntry: Codable, Equatable, Sendable {
    public let id: String
    public let title: String
    public let subtitle: String
    public let keywords: [String]
    public let section: String

    public init(id: String, title: String, subtitle: String = "", keywords: [String] = [], section: String) {
        self.id = id
        self.title = title
        self.subtitle = subtitle
        self.keywords = keywords
        self.section = section
    }
}

/// The JSON SearchScreens takes: {"screens":[…]}.
public struct ScreenEntries: Codable, Equatable, Sendable {
    public let screens: [ScreenEntry]

    public init(screens: [ScreenEntry]) { self.screens = screens }

    public var json: String { (try? TalosJSON.encode(self)) ?? #"{"screens":[]}"# }
}

/// One ranked screen; `id` names the catalog entry it opens.
public struct ScreenMatch: Decodable, Equatable, Identifiable, Sendable {
    public let id: String
    public let title: String
    public let subtitle: String
    public let section: String
    public let score: Double

    private enum CodingKeys: String, CodingKey { case id, title, subtitle, section, score }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        title = try c.decode(String.self, forKey: .title)
        subtitle = try c.decodeIfPresent(String.self, forKey: .subtitle) ?? ""
        section = try c.decodeIfPresent(String.self, forKey: .section) ?? ""
        score = try c.decodeIfPresent(Double.self, forKey: .score) ?? 0
    }
}

/// What SearchScreens gives: the best matches, and how many matched in all.
public struct ScreenMatches: Decodable, Equatable, Sendable {
    public let results: [ScreenMatch]
    public let total: Int

    public static let empty = ScreenMatches(results: [], total: 0)

    public init(results: [ScreenMatch], total: Int) {
        self.results = results
        self.total = total
    }

    private enum CodingKeys: String, CodingKey { case results, total }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        results = try c.decodeIfPresent([ScreenMatch].self, forKey: .results) ?? []
        total = try c.decodeIfPresent(Int.self, forKey: .total) ?? results.count
    }
}

/// The nodes the active cluster's last overview lists, from the search's entries (its own
/// "overview" blob: the last known one, a refresh's or the demo's); none without one.
public func searchOverviewNodes(_ entries: [SearchEntry], context: String) -> [NodeOverview] {
    guard let entry = entries.first(where: { $0.context == context && $0.key == "overview" }),
          let overview = try? TalosJSON.decode(ClusterOverview.self, from: entry.json) else { return [] }
    return overview.nodes
}
