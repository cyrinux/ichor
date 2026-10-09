import Foundation

// Mirrors go/ichorgo/imagescan*.go.

public enum ImageScanPhase {
    public static let preparing = "preparing"
    public static let starting = "starting"
    public static let database = "database"
    public static let scanning = "scanning"
    public static let cleaning = "cleaning"
}

public enum ImageScanSource {
    public static let scan = "scan"
    public static let operatorReports = "operator"
}

/// What the core puts in an image it did not get to scan (stopped, failed before).
public let imageScanNotScanned = "not scanned"
/// The core's message for a scan that took longer than its deadline.
public let imageScanTimedOut = "image scan timed out"
/// The namespace the scan Job runs in.
public let imageScanNamespace = "ichor-imagescan"

/// The scan id of a node's system images (an app's scan uses the app's id).
public func systemImagesScanID(node: String) -> String { "talos:\(node)" }

/// The options of a scan of image refs with no pod behind them ("" when there are none).
public func imageScanOptions(images: [String]) throws -> String {
    images.isEmpty ? "" : try TalosJSON.encode(["images": images])
}

/// An image Talos runs on a node outside of any app (TalosSystemImages): kubelet, etcd…
public struct SystemImage: Decodable, Equatable, Identifiable, Sendable {
    /// installer, kubelet, etcd, apiServer, controllerManager, scheduler, coreDNS, proxy or system.
    public let role: String
    public let image: String
    /// What a scan pulls: repo@digest when the node has it, else `image`.
    public let ref: String
    public let digest: String

    public var id: String { ref }

    public init(role: String, image: String, ref: String, digest: String = "") {
        self.role = role
        self.image = image
        self.ref = ref
        self.digest = digest
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        role = try c.field(.role, "")
        image = try c.field(.image, "")
        ref = try c.field(.ref, "")
        digest = try c.field(.digest, "")
    }

    private enum CodingKeys: String, CodingKey { case role, image, ref, digest }
}

/// Trivy's severities, most severe first.
public enum VulnSeverity: String, CaseIterable, Codable, Sendable, WireEnum {
    case critical = "CRITICAL", high = "HIGH", medium = "MEDIUM", low = "LOW", unknown = "UNKNOWN"

    public static let wireFallback = VulnSeverity.unknown
}

/// The formats ImageScanExport writes: its name, the file extension.
public enum ImageScanFormat: String, CaseIterable, Identifiable, Sendable {
    case html, sarif, cyclonedx, csv, json

    public var id: String { rawValue }

    public var fileExtension: String {
        switch self {
        case .html: "html"
        case .sarif: "sarif.json"
        case .cyclonedx: "cdx.json"
        case .csv: "csv"
        case .json: "json"
        }
    }
}

public struct ImageScanProgress: Decodable, Equatable, Sendable {
    public let phase: String
    public let step: Int
    public let steps: Int
    public let image: String
    /// The container's waiting reason while the pod starts (ContainerCreating).
    public let message: String

    public init(phase: String = "", step: Int = 0, steps: Int = 0, image: String = "", message: String = "") {
        self.phase = phase
        self.step = step
        self.steps = steps
        self.image = image
        self.message = message
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        phase = try c.field(.phase, "")
        step = try c.field(.step, 0)
        steps = try c.field(.steps, 0)
        image = try c.field(.image, "")
        message = try c.field(.message, "")
    }

    private enum CodingKeys: String, CodingKey { case phase, step, steps, image, message }
}

public struct VulnSummary: Codable, Equatable, Sendable {
    public var critical: Int
    public var high: Int
    public var medium: Int
    public var low: Int
    public var unknown: Int
    /// With a fixed version.
    public var fixable: Int
    /// In the OS packages: a newer base image fixes them.
    public var os: Int

    public init(critical: Int = 0, high: Int = 0, medium: Int = 0, low: Int = 0, unknown: Int = 0, fixable: Int = 0, os: Int = 0) {
        self.critical = critical
        self.high = high
        self.medium = medium
        self.low = low
        self.unknown = unknown
        self.fixable = fixable
        self.os = os
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        critical = try c.field(.critical, 0)
        high = try c.field(.high, 0)
        medium = try c.field(.medium, 0)
        low = try c.field(.low, 0)
        unknown = try c.field(.unknown, 0)
        fixable = try c.field(.fixable, 0)
        os = try c.field(.os, 0)
    }

    private enum CodingKeys: String, CodingKey { case critical, high, medium, low, unknown, fixable, os }

    public var total: Int { critical + high + medium + low + unknown }

    public func count(_ severity: VulnSeverity) -> Int {
        switch severity {
        case .critical: critical
        case .high: high
        case .medium: medium
        case .low: low
        case .unknown: unknown
        }
    }

    public static func + (a: VulnSummary, b: VulnSummary) -> VulnSummary {
        VulnSummary(critical: a.critical + b.critical, high: a.high + b.high, medium: a.medium + b.medium, low: a.low + b.low,
                    unknown: a.unknown + b.unknown, fixable: a.fixable + b.fixable, os: a.os + b.os)
    }
}

public struct ImageVuln: Codable, Equatable, Hashable, Sendable {
    public let id: String
    public let package: String
    public let installed: String
    public let fixed: String
    public let severity: String
    public let title: String
    public let description: String
    public let url: String
    public let score: Double
    public let vector: String
    public let purl: String
    /// "debian 12.5" for an OS package, the file that brought a library ("usr/local/bin/app").
    public let target: String
    public let `class`: String
    public let type: String
    public let published: String

    public init(id: String, package: String, installed: String, fixed: String = "", severity: String, title: String = "",
                description: String = "", url: String = "", score: Double = 0, vector: String = "", purl: String = "",
                target: String = "", class: String = "", type: String = "", published: String = "") {
        self.id = id
        self.package = package
        self.installed = installed
        self.fixed = fixed
        self.severity = severity
        self.title = title
        self.description = description
        self.url = url
        self.score = score
        self.vector = vector
        self.purl = purl
        self.target = target
        self.class = `class`
        self.type = type
        self.published = published
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, "")
        package = try c.field(.package, "")
        installed = try c.field(.installed, "")
        fixed = try c.field(.fixed, "")
        severity = try c.field(.severity, "")
        title = try c.field(.title, "")
        description = try c.field(.description, "")
        url = try c.field(.url, "")
        score = try c.field(.score, 0)
        vector = try c.field(.vector, "")
        purl = try c.field(.purl, "")
        target = try c.field(.target, "")
        `class` = try c.field(.class, "")
        type = try c.field(.type, "")
        published = try c.field(.published, "")
    }

    private enum CodingKeys: String, CodingKey {
        case id, package, installed, fixed, severity, title, description, url, score, vector, purl, target, `class`, type, published
    }

    public var level: VulnSeverity { VulnSeverity(wire: severity) }
    public var fixable: Bool { !fixed.isEmpty }
    /// Unique within an image, as the core deduplicates findings.
    public var key: String { "\(id)\u{0}\(package)\u{0}\(installed)\u{0}\(target)" }
}

public struct ScannedImage: Codable, Equatable, Sendable {
    /// As the pods name it (tag).
    public let image: String
    /// What was scanned: repo@digest when known.
    public let ref: String
    public let digest: String
    public let os: String
    public let pods: [String]
    public let scannedAt: Int64
    public let error: String
    public let summary: VulnSummary
    public let vulnerabilities: [ImageVuln]

    public init(image: String, ref: String = "", digest: String = "", os: String = "", pods: [String] = [], scannedAt: Int64 = 0,
                error: String = "", summary: VulnSummary = VulnSummary(), vulnerabilities: [ImageVuln] = []) {
        self.image = image
        self.ref = ref
        self.digest = digest
        self.os = os
        self.pods = pods
        self.scannedAt = scannedAt
        self.error = error
        self.summary = summary
        self.vulnerabilities = vulnerabilities
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        image = try c.field(.image, "")
        ref = try c.field(.ref, "")
        digest = try c.field(.digest, "")
        os = try c.field(.os, "")
        pods = try c.field(.pods, [])
        scannedAt = try c.field(.scannedAt, 0)
        error = try c.field(.error, "")
        summary = try c.field(.summary, VulnSummary())
        vulnerabilities = try c.field(.vulnerabilities, [])
    }

    private enum CodingKeys: String, CodingKey { case image, ref, digest, os, pods, scannedAt, error, summary, vulnerabilities }

    /// The findings `filter` keeps, grouped by package and installed version, in report order.
    public func byPackage(_ filter: VulnFilter) -> [(package: String, vulns: [ImageVuln])] {
        var order: [String] = []
        var groups: [String: [ImageVuln]] = [:]
        for v in vulnerabilities where filter.keeps(v) {
            let key = "\(v.package) \(v.installed)"
            if groups[key] == nil { order.append(key) }
            groups[key, default: []].append(v)
        }
        return order.map { (package: $0, vulns: groups[$0] ?? []) }
    }
}

public struct ImageScanReport: Codable, Equatable, Sendable {
    /// ImageScanSource.scan or .operatorReports.
    public let source: String
    public let scanner: String
    public let started: Int64
    public let finished: Int64
    public let images: [ScannedImage]

    public init(source: String = "", scanner: String = "", started: Int64 = 0, finished: Int64 = 0, images: [ScannedImage] = []) {
        self.source = source
        self.scanner = scanner
        self.started = started
        self.finished = finished
        self.images = images
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        source = try c.field(.source, "")
        scanner = try c.field(.scanner, "")
        started = try c.field(.started, 0)
        finished = try c.field(.finished, 0)
        images = try c.field(.images, [])
    }

    private enum CodingKeys: String, CodingKey { case source, scanner, started, finished, images }

    public var summary: VulnSummary { images.reduce(VulnSummary()) { $0 + $1.summary } }
    public var failed: Int { images.filter { !$0.error.isEmpty }.count }
    /// When it was made: the latest image scan of an operator report, else when the scan ended (ms).
    public var madeAt: Int64 {
        let latest = images.map(\.scannedAt).max() ?? 0
        return source == ImageScanSource.operatorReports && latest > 0 ? latest : finished
    }
}

/// ImageScanOperatorReports' answer: `available` false without the Trivy Operator.
public struct OperatorReports: Decodable, Equatable, Sendable {
    public let available: Bool
    public let report: ImageScanReport

    public init(available: Bool = false, report: ImageScanReport = ImageScanReport()) {
        self.available = available
        self.report = report
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        available = try c.field(.available, false)
        report = try c.field(.report, ImageScanReport())
    }

    private enum CodingKeys: String, CodingKey { case available, report }
}

/// What the report shows: the fixable findings only, of the chosen severities.
public struct VulnFilter: Equatable, Sendable {
    public var fixableOnly: Bool
    public var severities: Set<VulnSeverity>

    public init(fixableOnly: Bool = true, severities: Set<VulnSeverity> = Set(VulnSeverity.allCases)) {
        self.fixableOnly = fixableOnly
        self.severities = severities
    }

    public func keeps(_ v: ImageVuln) -> Bool { (!fixableOnly || v.fixable) && severities.contains(v.level) }

    public mutating func toggle(_ severity: VulnSeverity) {
        if severities.contains(severity) { severities.remove(severity) } else { severities.insert(severity) }
    }
}

/// A scan of one app's images: `running` until the core reports it done. `reportJSON` is the
/// core's report as is, what the exports are written from.
public struct ImageScanState: Equatable, Sendable {
    /// The cluster context and app it scans.
    public let context: String
    public let appID: String
    public var progress: ImageScanProgress?
    public var report: ImageScanReport?
    public var reportJSON = ""
    public var running = true
    /// Stop was asked: the core is deleting the scan Job.
    public var stopping = false
    /// Ended by Stop: its report holds only what was scanned before.
    public var stopped = false
    public var error: String?

    public init(context: String, appID: String) {
        self.context = context
        self.appID = appID
    }

    /// The finished scan's report, when it scanned at least one image (not stopped or failed early).
    public var usableReport: ImageScanReport? {
        guard let report, !running, !stopped, error == nil, report.images.contains(where: { $0.error.isEmpty }) else { return nil }
        return report
    }

    /// The core's report and error once it is done; stopping is not a failure.
    public mutating func finish(report: ImageScanReport?, json: String, error: String?) {
        self.report = report
        reportJSON = json
        running = false
        stopped = stopping
        self.error = stopping ? nil : error
    }
}

/// A file name for an exported report: the app, "vulnerabilities" and the date, kept to safe characters.
public func imageScanFilename(app: String, date: Date, format: ImageScanFormat) -> String {
    let allowed = Set("abcdefghijklmnopqrstuvwxyz0123456789._-")
    var safe = String(app.lowercased().map { allowed.contains($0) ? $0 : "-" })
    while safe.contains("--") { safe = safe.replacingOccurrences(of: "--", with: "-") }
    safe = safe.trimmingCharacters(in: CharacterSet(charactersIn: "-"))
    let stamp = DateFormatter()
    stamp.locale = Locale(identifier: "en_US_POSIX")
    stamp.timeZone = TimeZone.current
    stamp.dateFormat = "yyyyMMdd-HHmm"
    return "\(safe.isEmpty ? "app" : safe)-vulnerabilities-\(stamp.string(from: date)).\(format.fileExtension)"
}
