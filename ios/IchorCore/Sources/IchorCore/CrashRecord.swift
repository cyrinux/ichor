import Foundation

/// One diagnostic of a MetricKit payload (MXDiagnosticPayload.jsonRepresentation()): a crash,
/// a hang, a CPU or disk-write exception or a slow launch, with the top frames of its call
/// stack as `binary+offset` (not symbolicated on the phone). Parsed here, without MetricKit,
/// so it is tested on Linux; the app target alone imports MetricKit.
public struct CrashRecord: Equatable, Sendable {
    public enum Kind: String, CaseIterable, Sendable {
        case crash
        case hang
        case cpuException = "cpu_exception"
        case diskWriteException = "disk_write_exception"
        case launch = "app_launch"

        /// The payload's array of this kind.
        var payloadKey: String {
            switch self {
            case .crash: "crashDiagnostics"
            case .hang: "hangDiagnostics"
            case .cpuException: "cpuExceptionDiagnostics"
            case .diskWriteException: "diskWriteExceptionDiagnostics"
            case .launch: "appLaunchDiagnostics"
            }
        }
    }

    /// How many frames are kept.
    public static let maxFrames = 10

    public let kind: Kind
    /// The payload's time range, as MetricKit writes it ("2026-10-28 09:00:00").
    public let begin: String
    public let end: String
    /// "1.42.0 (142)".
    public let appVersion: String
    public let osVersion: String
    /// "Namespace SIGNAL, Code 0xb"; "" when MetricKit gives none.
    public let terminationReason: String
    public let exceptionType: Int?
    public let exceptionCode: Int?
    public let signal: Int?
    /// "8 sec" for a hang (also the CPU time or the writes of an exception, the launch time).
    public let duration: String
    /// The top frames of the attributed thread, innermost first: "Ichor+120424".
    public let frames: [String]

    /// Every diagnostic of a payload's JSON, in kind order; none when it is not one.
    public static func summarize(metricKitJSON: String) -> [CrashRecord] {
        guard
            let data = metricKitJSON.data(using: .utf8),
            let root = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any]
        else { return [] }
        let begin = root["timeStampBegin"] as? String ?? ""
        let end = root["timeStampEnd"] as? String ?? ""
        return Kind.allCases.flatMap { kind in
            (root[kind.payloadKey] as? [[String: Any]] ?? []).map {
                record(kind: kind, $0, begin: begin, end: end)
            }
        }
    }

    private static func record(kind: Kind, _ diagnostic: [String: Any], begin: String, end: String) -> CrashRecord {
        let meta = diagnostic["diagnosticMetaData"] as? [String: Any] ?? [:]
        let version = meta["appVersion"] as? String ?? ""
        let build = meta["appBuildVersion"] as? String ?? ""
        let duration = ["hangDuration", "totalCPUTime", "writesCaused", "launchDuration"]
            .lazy.compactMap { meta[$0] as? String }.first
        return CrashRecord(
            kind: kind, begin: begin, end: end,
            appVersion: build.isEmpty ? version : "\(version) (\(build))",
            osVersion: meta["osVersion"] as? String ?? "",
            terminationReason: meta["terminationReason"] as? String ?? "",
            exceptionType: int(meta["exceptionType"]), exceptionCode: int(meta["exceptionCode"]),
            signal: int(meta["signal"]), duration: duration ?? "",
            frames: frames(diagnostic["callStackTree"] as? [String: Any]))
    }

    /// The attributed thread's stack (the first one when none is), from its root frame down
    /// its first sub-frames.
    private static func frames(_ tree: [String: Any]?) -> [String] {
        let stacks = tree?["callStacks"] as? [[String: Any]] ?? []
        let stack = stacks.first { $0["threadAttributed"] as? Bool == true } ?? stacks.first
        var frame = (stack?["callStackRootFrames"] as? [[String: Any]])?.first
        var out: [String] = []
        while let current = frame, out.count < maxFrames {
            let binary = current["binaryName"] as? String ?? "?"
            out.append("\(binary)+\(int(current["offsetIntoBinaryTextSegment"]) ?? 0)")
            frame = (current["subFrames"] as? [[String: Any]])?.first
        }
        return out
    }

    /// A JSON number (NSNumber on Linux, Int on Darwin) as an Int.
    private static func int(_ value: Any?) -> Int? {
        if let n = value as? Int { return n }
        if let n = value as? NSNumber { return n.intValue }
        if let d = value as? Double { return Int(d) }
        return nil
    }

    /// The ring line's attributes: the kind and numbers only, never a frame or a path.
    public var ringAttrs: [String: DiagValue] {
        var out: [String: DiagValue] = ["kind": .text(kind.rawValue), "frames": .number(Double(frames.count))]
        if let exceptionType { out["exception_type"] = .number(Double(exceptionType)) }
        if let exceptionCode { out["exception_code"] = .number(Double(exceptionCode)) }
        if let signal { out["signal"] = .number(Double(signal)) }
        if !duration.isEmpty { out["duration"] = .text(duration) }
        return out
    }

    /// "Signal 11 · exception 1 (code 0)": what the preview shows under the reason.
    public var codesLine: String {
        var parts: [String] = []
        if let signal { parts.append("signal \(signal)") }
        if let exceptionType {
            parts.append("exception \(exceptionType)" + (exceptionCode.map { " (code \($0))" } ?? ""))
        }
        if !duration.isEmpty { parts.append(duration) }
        return parts.joined(separator: " · ")
    }
}
