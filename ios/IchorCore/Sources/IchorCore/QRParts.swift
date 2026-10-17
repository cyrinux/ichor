import Foundation

/// One code of a config split over several QR codes (`ichor:<i>/<n>:<chunk>`, Go QRPart):
/// its 1-based index, the size of its set and its base64url gzip chunk. The chunk is part of a
/// credential: never log it.
public struct QRPart: Codable, Equatable, Sendable {
    public let index: Int
    public let total: Int
    public let chunk: String

    public init(index: Int, total: Int, chunk: String) {
        self.index = index
        self.total = total
        self.chunk = chunk
    }
}

/// A multi-part QR error of the Go core, whose messages start with a code (qr_parts.go).
public enum QRPartsError: Error, Equatable, Sendable {
    /// Parts of different sets (a different count, or another chunk for an index).
    case mismatch
    /// The set lacks parts.
    case missing
    /// A malformed part, or chunks that do not expand into a config.
    case invalid

    public init?(coreMessage: String) {
        let codes: [(String, QRPartsError)] = [
            ("qr-parts-mismatch", .mismatch),
            ("qr-parts-missing", .missing),
            ("qr-parts-invalid", .invalid),
        ]
        guard let match = codes.first(where: { coreMessage.hasPrefix($0.0) }) else { return nil }
        self = match.1
    }
}

/// What scanning one code did to the collected set.
public enum QRPartOutcome: Equatable, Sendable {
    /// Not a part: the scanner keeps its one-code path.
    case notPart
    /// A part already collected (the camera sees the same code again).
    case duplicate
    /// A new part; the set still lacks some.
    case added
    /// A part of another set (a different count, or another chunk for a collected index):
    /// the set restarted from it.
    case restarted
    /// The last part: the import text, as a one-code scan gives it.
    case complete(String)
    /// The part or the set is unusable; the collector is empty again.
    case failed(QRPartsError)
}

/// Collects the parts of a multi-part QR import in any order (Go QRPart / QRPartsJoin are
/// passed in, so this runs without gomobile). Each closure throws an error whose
/// LocalizedError description is the Go message.
public struct QRPartCollector: Sendable {
    public private(set) var total = 0
    private var chunks: [Int: String] = [:]

    public init() {}

    /// How many distinct parts are in.
    public var received: Int { chunks.count }

    /// The indexes still to scan, in order.
    public var missing: [Int] {
        total > 0 ? (1...total).filter { chunks[$0] == nil } : []
    }

    /// True once a part is in, until the set completes, fails or is reset.
    public var isCollecting: Bool { total > 0 }

    public mutating func reset() {
        self = QRPartCollector()
    }

    /// Adds a scanned code's text. `part` is Go QRPart (JSON, "" when not a part), `join` is
    /// Go QRPartsJoin (the parts as a JSON array → the import text).
    public mutating func add(
        _ text: String,
        part: (String) throws -> String,
        join: (String) throws -> String
    ) -> QRPartOutcome {
        let parsed: QRPart
        do {
            let json = try part(text)
            if json.isEmpty { return .notPart }
            guard let decoded = try? JSONDecoder().decode(QRPart.self, from: Data(json.utf8)),
                  decoded.total > 0, (1...decoded.total).contains(decoded.index)
            else { return .failed(.invalid) }
            parsed = decoded
        } catch {
            return .failed(Self.mapped(error))
        }
        return accept(parsed, join: join)
    }

    private mutating func accept(_ part: QRPart, join: (String) throws -> String) -> QRPartOutcome {
        if let known = chunks[part.index], part.total == total {
            if known == part.chunk { return .duplicate }
        }
        let restarted = isCollecting && (part.total != total || chunks[part.index] != nil)
        if restarted { reset() }
        total = part.total
        chunks[part.index] = part.chunk
        guard missing.isEmpty else { return restarted ? .restarted : .added }
        return finish(join: join)
    }

    private mutating func finish(join: (String) throws -> String) -> QRPartOutcome {
        let parts = (1...total).compactMap { index in
            chunks[index].map { QRPart(index: index, total: total, chunk: $0) }
        }
        reset()
        do {
            let json = String(decoding: try JSONEncoder().encode(parts), as: UTF8.self)
            return .complete(try join(json))
        } catch {
            return .failed(Self.mapped(error))
        }
    }

    /// The code of a Go error; any other failure counts as an invalid part.
    private static func mapped(_ error: Error) -> QRPartsError {
        if let known = error as? QRPartsError { return known }
        let message = (error as? LocalizedError)?.errorDescription ?? ""
        return QRPartsError(coreMessage: message) ?? .invalid
    }
}
