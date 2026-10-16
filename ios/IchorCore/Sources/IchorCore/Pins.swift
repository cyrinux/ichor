import Foundation

/// A screen kept on the overview's Pinned card: the share link's target (without its cluster:
/// pins are kept per cluster) and the name shown for it. Same JSON as Android's `Pin`:
/// `{"target": {...share target...}, "label": "..."}`. Holds nothing a share link would not carry.
public struct Pin: Codable, Hashable, Sendable, Identifiable {
    public let target: ShareTarget
    public let label: String

    public init(target: ShareTarget, label: String) {
        self.target = Pin.normalized(target)
        self.label = label
    }

    private enum CodingKeys: String, CodingKey { case target, label }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        target = Pin.normalized(try c.decode(ShareTarget.self, forKey: .target))
        label = try c.decodeIfPresent(String.self, forKey: .label) ?? ""
    }

    /// The object pinned, whatever the label or the tab it was pinned from: one pin per object
    /// (Android's `Pin.key`).
    public var id: String { Pin.key(target) }

    /// Whether this pin names `other`'s object (its cluster and tab left aside).
    public func pins(_ other: ShareTarget) -> Bool { id == Pin.key(other) }

    /// The kinds of screens offered to pin: a node, a workload, a pod, a CronJob, a GitOps app.
    public static let pinnable: Set<ShareTarget.Target> = [.node, .workload, .pod, .cronJob, .argoApp, .fluxApp]

    private static func key(_ target: ShareTarget) -> String {
        [target.target.rawValue, target.host, target.addr, target.kind, target.namespace, target.name].joined(separator: "|")
    }

    private static func normalized(_ target: ShareTarget) -> ShareTarget {
        var plain = target
        plain.cluster = ""
        return plain
    }
}

/// A cluster's pins in the order they were added, at most `limit`. Saved as a JSON array
/// under `storageKey(fingerprint:)`, read the same way by Android.
public struct PinList: Equatable, Sendable {
    public static let limit = 12

    /// The `@AppStorage` key of the pins of the cluster with this context fingerprint.
    public static func storageKey(fingerprint: String) -> String { "pins.\(fingerprint)" }

    public let pins: [Pin]

    public init(_ pins: [Pin] = []) {
        var seen: Set<String> = []
        self.pins = Array(pins.filter { seen.insert($0.id).inserted }.prefix(PinList.limit))
    }

    public var isEmpty: Bool { pins.isEmpty }
    public var isFull: Bool { pins.count >= PinList.limit }

    public func contains(_ target: ShareTarget) -> Bool { pins.contains { $0.pins(target) } }

    /// Without the pin of `pin`'s screen when there is one, else with `pin` added last; full: unchanged.
    public func toggling(_ pin: Pin) -> PinList {
        if contains(pin.target) { return removing(pin.target) }
        guard !isFull else { return self }
        return PinList(pins + [pin])
    }

    public func removing(_ target: ShareTarget) -> PinList { PinList(pins.filter { !$0.pins(target) }) }

    /// The JSON array saved; empty text for no pins.
    public var encoded: String {
        guard !pins.isEmpty else { return "" }
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys, .withoutEscapingSlashes]
        guard let data = try? encoder.encode(pins) else { return "" }
        return String(decoding: data, as: UTF8.self)
    }

    /// Reads `encoded`'s form; a pin this version cannot read (a newer target) or cannot pin
    /// is skipped, anything else unreadable gives no pins.
    public static func parse(_ text: String?) -> PinList {
        guard let text, let data = text.data(using: .utf8), !data.isEmpty,
              let read = try? JSONDecoder().decode([LenientPin].self, from: data) else { return PinList() }
        return PinList(read.compactMap(\.pin).filter { Pin.pinnable.contains($0.target.target) })
    }

    private struct LenientPin: Decodable {
        let pin: Pin?
        init(from decoder: Decoder) throws { pin = try? Pin(from: decoder) }
    }
}
