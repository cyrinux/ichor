import Foundation

// Helm releases, read-only (go/ichorgo/kube_helm.go): what `helm list` and `helm get` show,
// decoded from the release Secrets by the Go core.

/// The latest revision of a release (KubeHelmReleases).
public struct HelmReleaseSummary: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let name: String
    public let namespace: String
    public let revision: Int
    /// deployed, failed, pending-install, superseded...
    public let status: String
    public let chart: String
    public let chartVersion: String
    public let appVersion: String
    /// Unix seconds, 0 when unknown.
    public let updated: Int64

    public var id: String { "\(namespace)/\(name)" }

    public init(name: String, namespace: String, revision: Int = 1, status: String = "deployed", chart: String = "",
                chartVersion: String = "", appVersion: String = "", updated: Int64 = 0) {
        self.name = name
        self.namespace = namespace
        self.revision = revision
        self.status = status
        self.chart = chart
        self.chartVersion = chartVersion
        self.appVersion = appVersion
        self.updated = updated
    }

    private enum CodingKeys: String, CodingKey { case name, namespace, revision, status, chart, chartVersion, appVersion, updated }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        namespace = try c.field(.namespace, "")
        revision = try c.field(.revision, 0)
        status = try c.field(.status, "")
        chart = try c.field(.chart, "")
        chartVersion = try c.field(.chartVersion, "")
        appVersion = try c.field(.appVersion, "")
        updated = try c.field(.updated, 0)
    }

    /// "cert-manager-v1.18.2", as `helm list` shows the chart.
    public var chartLabel: String {
        chartVersion.isEmpty ? chart : "\(chart)-\(chartVersion)"
    }

    public var tone: KubeTone { kubeStatusTone(status) }
}

public struct HelmReleaseList: Decodable, Equatable, Sendable {
    public let releases: [HelmReleaseSummary]

    public init(releases: [HelmReleaseSummary] = []) { self.releases = releases }

    private enum CodingKeys: String, CodingKey { case releases }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        releases = try c.field(.releases, [])
    }
}

/// A revision of a release, as `helm history` lists it.
public struct HelmRevision: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let revision: Int
    public let status: String
    /// Unix seconds.
    public let updated: Int64
    public let description: String

    public var id: Int { revision }

    public init(revision: Int, status: String = "", updated: Int64 = 0, description: String = "") {
        self.revision = revision
        self.status = status
        self.updated = updated
        self.description = description
    }

    private enum CodingKeys: String, CodingKey { case revision, status, updated, description }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        revision = try c.field(.revision, 0)
        status = try c.field(.status, "")
        updated = try c.field(.updated, 0)
        description = try c.field(.description, "")
    }

    public var tone: KubeTone { kubeStatusTone(status) }
}

/// One release in full (KubeHelmRelease): its latest revision's values, notes and manifest,
/// and its history.
public struct HelmReleaseDetail: Decodable, Equatable, Sendable {
    public let summary: HelmReleaseSummary
    public let description: String
    public let notes: String
    /// The values the user set (YAML), "" when none.
    public let values: String
    public let manifest: String
    /// Newest first.
    public let history: [HelmRevision]

    public init(summary: HelmReleaseSummary, description: String = "", notes: String = "", values: String = "",
                manifest: String = "", history: [HelmRevision] = []) {
        self.summary = summary
        self.description = description
        self.notes = notes
        self.values = values
        self.manifest = manifest
        self.history = history
    }

    private enum CodingKeys: String, CodingKey { case description, notes, values, manifest, history }

    public init(from decoder: Decoder) throws {
        // The summary's fields sit at the top level, next to these.
        summary = try HelmReleaseSummary(from: decoder)
        let c = try decoder.container(keyedBy: CodingKeys.self)
        description = try c.field(.description, "")
        notes = try c.field(.notes, "")
        values = try c.field(.values, "")
        manifest = try c.field(.manifest, "")
        let revisions: [HelmRevision] = try c.field(.history, [])
        history = revisions.sorted { $0.revision > $1.revision }
    }
}

/// Releases whose name, namespace or chart contains `query` (case-insensitive; all when blank).
public func filterHelmReleases(_ releases: [HelmReleaseSummary], query: String) -> [HelmReleaseSummary] {
    let needle = query.trimmingCharacters(in: .whitespaces)
    guard !needle.isEmpty else { return releases }
    return releases.filter { release in
        [release.name, release.namespace, release.chart].contains { $0.range(of: needle, options: .caseInsensitive) != nil }
    }
}

/// The releases needing a look first (failed, then pending), then by namespace and name.
public func sortHelmReleases(_ releases: [HelmReleaseSummary]) -> [HelmReleaseSummary] {
    let rank = { (tone: KubeTone) -> Int in
        switch tone {
        case .bad: 0
        case .warn: 1
        case .good, .neutral: 2
        }
    }
    return releases.sorted {
        (rank($0.tone), $0.namespace, $0.name) < (rank($1.tone), $1.namespace, $1.name)
    }
}
