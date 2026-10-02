import Foundation

/// Roles a generated talosconfig may carry (the Go core rejects anything else).
public let issuableRoles = ["os:admin", "os:operator", "os:reader", "os:etcd:backup"]

/// Default roles of a config made for another device: read-only.
public let defaultIssuedRoles: Set<String> = ["os:reader"]

/// Certificate lifetimes offered when issuing a talosconfig; same presets as Android.
public enum ConfigValidity: Int, CaseIterable, Identifiable, Sendable {
    case days30 = 30, days90 = 90, year = 365

    public static let `default` = ConfigValidity.year

    public var id: Int { rawValue }
    public var days: Int { rawValue }
    public var hours: Int { rawValue * 24 }
}

/// The comma-separated role list for GenerateTalosconfig, in `issuableRoles` order; nil when
/// no issuable role is selected (unknown roles are dropped).
public func rolesArgument(_ roles: some Sequence<String>) -> String? {
    let wanted = Set(roles)
    let list = issuableRoles.filter { wanted.contains($0) }
    return list.isEmpty ? nil : list.joined(separator: ",")
}

/// Suggested file name, e.g. "talosconfig-lab-reader.yaml" (same as Android).
public func issuedFileName(context: String, roles: String) -> String {
    let allowed = Set("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-")
    let safe = String(context.map { allowed.contains($0) ? $0 : "_" })
    let suffix = roles.split(separator: ",")
        .map { String($0.hasPrefix("os:") ? $0.dropFirst(3) : $0[...]).replacingOccurrences(of: ":", with: "-") }
        .joined(separator: "-")
    let name = safe.isEmpty ? "cluster" : safe
    return suffix.isEmpty ? "talosconfig-\(name).yaml" : "talosconfig-\(name)-\(suffix).yaml"
}

/// Largest QR payload: version 40 with error correction L holds 2953 bytes in byte mode.
public let maxQRPayloadBytes = 2_953

/// Whether a talosconfig fits in a single scannable QR code (CIQRCodeGenerator, level L).
public func fitsInQRCode(_ text: String) -> Bool {
    text.utf8.count <= maxQRPayloadBytes
}
