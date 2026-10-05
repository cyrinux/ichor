import Foundation

// The KubeSpan map's geometry, same as Android's ui/kubespan/TopologyLayout.kt: pure, in points.

/// A point of the map, in points from its top left corner.
public struct MapPoint: Equatable, Sendable {
    public let x: Double
    public let y: Double

    public init(x: Double, y: Double) {
        self.x = x
        self.y = y
    }
}

public struct SiteBox: Equatable, Sendable {
    public let site: TopologySite
    public let top: Double
    public let height: Double

    public init(site: TopologySite, top: Double, height: Double) {
        self.site = site
        self.top = top
        self.height = height
    }
}

/// Where the map draws everything for a map `width` wide: sites stacked as boxes, each with a
/// header row then its nodes on a grid of up to `maxColumns` per row.
public struct TopologyLayout: Equatable, Sendable {
    public static let maxColumns = 3
    public static let siteGap = 16.0
    public static let headerHeight = 36.0
    public static let cellHeight = 60.0
    public static let sitePadding = 8.0

    public let width: Double
    public let height: Double
    public let sites: [SiteBox]
    public let nodes: [String: MapPoint]
    /// Width a node chip may take.
    public let cellWidth: Double
    /// Height of a site header row.
    public let header: Double

    public init(width: Double, height: Double, sites: [SiteBox], nodes: [String: MapPoint],
                cellWidth: Double, header: Double = TopologyLayout.headerHeight) {
        self.width = width
        self.height = height
        self.sites = sites
        self.nodes = nodes
        self.cellWidth = cellWidth
        self.header = header
    }

    /// Control point of link a-b: straight inside a site, bowed to the right across sites.
    public func control(_ a: MapPoint, _ b: MapPoint, sameSite: Bool) -> MapPoint {
        let mid = MapPoint(x: (a.x + b.x) / 2, y: (a.y + b.y) / 2)
        if sameSite { return mid }
        let bow = min(width * 0.45, abs(b.y - a.y) * 0.35 + 24)
        return MapPoint(x: min(width - 8, mid.x + bow), y: mid.y)
    }

    /// The ends and control point of `link`, nil when an end is not on the map.
    public func curve(of link: TopologyLink, in topology: ClusterTopology) -> (a: MapPoint, c: MapPoint, b: MapPoint)? {
        guard let a = nodes[link.a], let b = nodes[link.b] else { return nil }
        let siteA = topology.nodes.first { $0.id == link.a }?.site
        let siteB = topology.nodes.first { $0.id == link.b }?.site
        return (a, control(a, b, sameSite: siteA == siteB), b)
    }

    /// The middle of the link's curve, where its measured speed is shown: (a + 2c + b) / 4.
    public func middle(of link: TopologyLink, in topology: ClusterTopology) -> MapPoint? {
        guard let (a, c, b) = curve(of: link, in: topology) else { return nil }
        return quadratic(a, c, b, t: 0.5)
    }

    /// The link whose curve passes within `slop` points of `at`, the closest one; nil when none
    /// does. Curves are sampled, which is plenty for a tap.
    public func linkAt(_ topology: ClusterTopology, at: MapPoint, slop: Double) -> Int? {
        let sites = Dictionary(topology.nodes.map { ($0.id, $0.site) }, uniquingKeysWith: { first, _ in first })
        var best: Int?
        var bestDistance = slop
        for (index, link) in topology.links.enumerated() {
            guard let a = nodes[link.a], let b = nodes[link.b] else { continue }
            let c = control(a, b, sameSite: sites[link.a] == sites[link.b])
            for step in 0...linkSamples {
                let p = quadratic(a, c, b, t: Double(step) / Double(linkSamples))
                let d = hypot(p.x - at.x, p.y - at.y)
                if d < bestDistance {
                    bestDistance = d
                    best = index
                }
            }
        }
        return best
    }
}

/// The map's layout for a map `width` wide; `textScale` (the Dynamic Type scale, at least 1)
/// makes rows grow with the chips in them.
public func topologyLayout(_ topology: ClusterTopology, width: Double, textScale: Double = 1) -> TopologyLayout {
    let scale = max(textScale, 1)
    let header = TopologyLayout.headerHeight * scale
    let cellHeight = TopologyLayout.cellHeight * scale
    var boxes: [SiteBox] = []
    var points: [String: MapPoint] = [:]
    var top = 0.0
    let widest = topology.sites.map { min($0.nodes.count, TopologyLayout.maxColumns) }.max() ?? 1
    for site in topology.sites {
        let columns = min(max(site.nodes.count, 1), TopologyLayout.maxColumns)
        let rows = max(Int((Double(site.nodes.count) / Double(columns)).rounded(.up)), 1)
        let height = header + Double(rows) * cellHeight + TopologyLayout.sitePadding
        for (i, id) in site.nodes.enumerated() {
            let row = i / columns
            let inRow = min(columns, site.nodes.count - row * columns)
            let cell = width / Double(inRow)
            points[id] = MapPoint(
                x: cell * (Double(i % columns) + 0.5),
                y: top + header + cellHeight * (Double(row) + 0.5)
            )
        }
        boxes.append(SiteBox(site: site, top: top, height: height))
        top += height + TopologyLayout.siteGap
    }
    return TopologyLayout(
        width: width,
        height: max(top - TopologyLayout.siteGap, 0),
        sites: boxes,
        nodes: points,
        cellWidth: width / Double(max(widest, 1)),
        header: header
    )
}

/// The point at `t` (0...1) on the quadratic curve from `a` to `b` with control point `c`.
public func quadratic(_ a: MapPoint, _ c: MapPoint, _ b: MapPoint, t: Double) -> MapPoint {
    let u = 1 - t
    return MapPoint(
        x: u * u * a.x + 2 * u * t * c.x + t * t * b.x,
        y: u * u * a.y + 2 * u * t * c.y + t * t * b.y
    )
}

private let linkSamples = 24

// MARK: - Network test from the map

extension Array where Element == String {
    /// The pair picked on the map after a tap on node `id`: the first node picked is the client,
    /// the second the server. Tapping a picked node drops it; a third node starts a new pair.
    public func pickingNode(_ id: String) -> [String] {
        if contains(id) { return filter { $0 != id } }
        if count >= 2 { return [id] }
        return self + [id]
    }
}

extension NetPerfSetup {
    /// This setup testing from `client` to `server`, checked against `nodes` like `withNodes`;
    /// as asked while the node list is not loaded (it is checked once it is).
    public func between(client: String, server: String, nodes: [NetPerfNode]?) -> NetPerfSetup {
        var asked = self
        asked.client = client
        asked.server = server
        return nodes.map(asked.withNodes) ?? asked
    }
}

extension NetPerfReport {
    /// Pod-to-pod throughput of this test, nil when not measured.
    public var podThroughputMbps: Double? {
        results.first { $0.path == NetPerfPath.pod && $0.test == NetPerfTest.throughput && $0.error.isEmpty }?.throughputMbps
    }
}

extension Array where Element == NetPerfReport {
    /// The newest saved test between nodes `a` and `b`, either way round, that measured pod-to-pod
    /// throughput: what the KubeSpan map shows on their link. Nil when they were never tested.
    public func latestBetween(_ a: String, _ b: String) -> NetPerfReport? {
        filter { ($0.client == a && $0.server == b) || ($0.client == b && $0.server == a) }
            .filter { $0.podThroughputMbps != nil }
            .max { $0.started < $1.started }
    }
}
