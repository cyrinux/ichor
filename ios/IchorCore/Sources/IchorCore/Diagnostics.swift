import Foundation

// Mirrors go/ichorgo/diag.go and diag_export.go: the core's redacted diagnostics log and the
// zip made from it, on this device only. The keys Go always writes have no default, so an
// entry missing one fails to decode instead of showing an empty row.

/// A log attribute value: numbers, booleans and short words only (DiagLog refuses the rest).
public enum DiagValue: Codable, Equatable, Sendable {
    case number(Double)
    case bool(Bool)
    case text(String)

    public init(from decoder: Decoder) throws {
        let c = try decoder.singleValueContainer()
        if let b = try? c.decode(Bool.self) {
            self = .bool(b)
        } else if let n = try? c.decode(Double.self) {
            self = .number(n)
        } else {
            self = .text(try c.decode(String.self))
        }
    }

    public func encode(to encoder: Encoder) throws {
        var c = encoder.singleValueContainer()
        switch self {
        case .number(let n):
            // Whole numbers as integers: "12", not "12.0".
            if n.rounded() == n, abs(n) < 1e15 { try c.encode(Int64(n)) } else { try c.encode(n) }
        case .bool(let b): try c.encode(b)
        case .text(let s): try c.encode(s)
        }
    }

    /// The value as the preview shows it: "12", "true", "ClusterOverview".
    public var text: String {
        switch self {
        case .number(let n): n.rounded() == n && abs(n) < 1e15 ? String(Int64(n)) : String(n)
        case .bool(let b): b ? "true" : "false"
        case .text(let s): s
        }
    }
}

/// One entry of the diagnostics log (DiagEntries), already redacted by the core.
public struct DiagEntry: Decodable, Equatable, Sendable {
    /// Unix milliseconds.
    public let at: Int64
    /// debug, info, warn or error.
    public let level: String
    /// kube, talos, gitops, net, store, trace, api, android, ios.
    public let component: String
    public let msg: String
    public let attrs: [String: DiagValue]
    public let err: String

    private enum CodingKeys: String, CodingKey { case at, level, component, msg, attrs, err }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        at = try c.decode(Int64.self, forKey: .at)
        level = try c.decode(String.self, forKey: .level)
        component = try c.decode(String.self, forKey: .component)
        msg = try c.decode(String.self, forKey: .msg)
        attrs = try c.decodeIfPresent([String: DiagValue].self, forKey: .attrs) ?? [:]
        err = try c.decodeIfPresent(String.self, forKey: .err) ?? ""
    }

    public var date: Date { Date(timeIntervalSince1970: Double(at) / 1000) }

    /// "fn=ClusterOverview ms=12", by key.
    public var attrsLine: String {
        attrs.keys.sorted().map { "\($0)=\(attrs[$0]?.text ?? "")" }.joined(separator: " ")
    }

    /// warn and error entries stand out in the preview.
    public var isProblem: Bool { level == "warn" || level == "error" }
}

/// What DiagExport returns: where the zip is, its size, the log entries in it and its members.
public struct DiagExportSummary: Decodable, Equatable, Sendable {
    public let path: String
    public let bytes: Int64
    public let entries: Int
    public let files: [String]
}

/// The app's own facts: app.json in the zip. Never a path the user chose, a cluster or an account.
public struct AppInfo: Encodable, Equatable, Sendable {
    public let version: String
    public let build: String
    /// "iOS" or "iPadOS".
    public let system: String
    /// "18.1".
    public let systemVersion: String
    /// The hardware model from utsname: "iPhone17,1".
    public let model: String
    /// A language tag such as "fr-FR".
    public let locale: String
    /// Whether the core keeps its data, and why not.
    public let dataStore: DataStoreStatus

    public init(
        version: String?, build: String?, system: String, systemVersion: String, model: String?, locale: String?,
        dataStore: DataStoreStatus?
    ) {
        self.version = version ?? ""
        self.build = build ?? ""
        self.system = system
        self.systemVersion = systemVersion
        self.model = model ?? ""
        self.locale = locale ?? ""
        self.dataStore = dataStore ?? DataStoreStatus(persisting: false)
    }

    /// app.json: every key written, none null.
    public func json() -> String {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        return (try? encoder.encode(self)).map { String(decoding: $0, as: UTF8.self) } ?? "{}"
    }

    /// The hardware model of `utsname.machine`, a fixed-size C char tuple.
    public static func machine<T>(_ tuple: T) -> String {
        withUnsafeBytes(of: tuple) { raw in
            String(decoding: raw.prefix { $0 != 0 }, as: UTF8.self)
        }
    }
}

/// Names, settings and JSON of the diagnostics the app keeps and exports.
public enum DiagnosticsFiles {
    /// UserDefaults key of "Keep a diagnostics log on this device" (off by default).
    public static let persistKey = "diagPersist"
    /// How many MetricKit payloads are kept.
    public static let keptPayloads = 10
    /// How many log entries the preview shows.
    public static let previewEntries = 200

    private static func formatter(_ format: String, _ timeZone: TimeZone) -> DateFormatter {
        let f = DateFormatter()
        f.locale = Locale(identifier: "en_US_POSIX")
        f.timeZone = timeZone
        f.dateFormat = format
        return f
    }

    /// "ichor-diagnostics-20261029-1412.zip" for `date`, in local time.
    public static func zipName(at date: Date, timeZone: TimeZone = .current) -> String {
        "ichor-diagnostics-\(formatter("yyyyMMdd-HHmm", timeZone).string(from: date)).zip"
    }

    /// "mx-20261029T141205Z-0.json": the `index`th MetricKit payload received at `date` (UTC),
    /// so names sort by date.
    public static func payloadName(at date: Date, index: Int) -> String {
        "mx-\(formatter("yyyyMMdd'T'HHmmss'Z'", TimeZone(identifier: "UTC")!).string(from: date))-\(index).json"
    }

    /// Whether `name` is a stored MetricKit payload.
    public static func isPayload(_ name: String) -> Bool {
        name.hasPrefix("mx-") && name.hasSuffix(".json")
    }

    /// The stored payloads among `names`, newest first.
    public static func payloads(in names: [String]) -> [String] {
        names.filter(isPayload).sorted(by: >)
    }

    /// The stored payloads to delete so only the newest `keep` stay.
    public static func pruned(_ names: [String], keep: Int = keptPayloads) -> [String] {
        Array(payloads(in: names).dropFirst(keep))
    }

    /// platform.json: the stored payloads (each MetricKit's own JSON), newest first, as one
    /// array. A payload that is not JSON is left out.
    public static func platformJSON(payloads: [Data]) -> String {
        let valid = payloads.filter { (try? JSONSerialization.jsonObject(with: $0)) is [String: Any] }
        return "[" + valid.map { String(decoding: $0, as: UTF8.self) }.joined(separator: ",") + "]"
    }

    /// `attrs` as the flat JSON object DiagLog takes ("{}" for none), keys sorted.
    public static func attrsJSON(_ attrs: [String: DiagValue]) -> String {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        return (try? encoder.encode(attrs)).map { String(decoding: $0, as: UTF8.self) } ?? "{}"
    }
}
