import Foundation

/// The Apps screen's chips: everything, what needs a look, or one category.
public enum AppFilter: Hashable, Sendable {
    case all, attention
    case category(AppCategory)
}

/// Apps matching the chip and the search (name, id, namespaces or image repositories,
/// case-insensitive); an empty query keeps everything. The order is kept.
public func filterApps(_ apps: [InventoryApp], query: String, filter: AppFilter) -> [InventoryApp] {
    let needle = query.trimmingCharacters(in: .whitespaces)
    return apps.filter { app in
        switch filter {
        case .all: break
        case .attention: if !app.needsAttention { return false }
        case .category(let category): if app.category != category { return false }
        }
        guard !needle.isEmpty else { return true }
        let fields = [app.name, app.id] + app.namespaces + app.images.map(\.repo)
        return fields.contains { $0.range(of: needle, options: .caseInsensitive) != nil }
    }
}

/// The screen's three groups: the grid, the Kubernetes and Talos plumbing, and the apps the
/// catalog does not recognise.
public struct InventorySections: Equatable, Sendable {
    public let main: [InventoryApp]
    public let system: [InventoryApp]
    public let unrecognised: [InventoryApp]

    public init(_ apps: [InventoryApp]) {
        main = apps.filter { !$0.system && $0.known }
        system = apps.filter(\.system)
        unrecognised = apps.filter { !$0.system && !$0.known }
    }
}

public struct AppCategoryCount: Equatable, Sendable {
    public let category: AppCategory
    public let count: Int
}

/// The categories present, in AppCategory's order (like the Android app).
public func categoryCounts(_ apps: [InventoryApp]) -> [AppCategoryCount] {
    let counts = Dictionary(grouping: apps, by: \.category).mapValues(\.count)
    return AppCategory.allCases.compactMap { category in
        counts[category].map { AppCategoryCount(category: category, count: $0) }
    }
}

/// The overview card's tiles: up to `limit` non-system apps, those with an icon first (a
/// remote one counts only when allowed), and how many are left out.
public func overviewTiles(_ apps: [InventoryApp], limit: Int, remoteIcons: Bool) -> (shown: [InventoryApp], rest: Int) {
    let candidates = apps.filter { !$0.system }
    let withIcon = candidates.filter { $0.iconSource(remoteIcons: remoteIcons) != .monogram }
    let without = candidates.filter { $0.iconSource(remoteIcons: remoteIcons) == .monogram }
    let shown = Array((withIcon + without).prefix(max(limit, 0)))
    return (shown, candidates.count - shown.count)
}
