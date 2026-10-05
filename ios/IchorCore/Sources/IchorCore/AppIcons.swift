import Foundation

/// Where an app's icon comes from.
public enum AppIconSource: Equatable, Sendable {
    /// appicons/<name>.webp in the app bundle (a -night variant may exist for dark themes).
    case bundled(String)
    /// A Dashboard Icons slug, see remoteIconURL.
    case remote(String)
    /// A resource's own icon at an https URL (fetched only when the user allowed downloads).
    case url(URL)
    /// A resource's own icon inline (a data: URI's bytes), never fetched.
    case inline(Data)
    /// Initials on a colored square, see monogramInitials and monogramHue.
    case monogram
}

extension InventoryApp {
    /// Its own icon, else the bundled icon, else the remote one when the user allowed downloads,
    /// else a monogram. A name that is not a valid icon slug is ignored, never used as a file or URL path.
    public func iconSource(remoteIcons: Bool) -> AppIconSource {
        if let iconURL, let custom = customIconSource(iconURL, remoteIcons: remoteIcons) { return custom }
        if let icon, isValidIconSlug(icon) { return .bundled(icon) }
        if remoteIcons, let remoteIcon, isValidIconSlug(remoteIcon) { return .remote(remoteIcon) }
        return .monogram
    }
}

/// Whether `slug` matches ^[a-z0-9][a-z0-9-]{0,80}$ (like the Android app): a bundled icon
/// name or a Dashboard Icons slug, safe in a resource name or a URL path.
public func isValidIconSlug(_ slug: String) -> Bool {
    let bytes = Array(slug.utf8)
    let lowerOrDigit: (UInt8) -> Bool = { (0x61...0x7A).contains($0) || (0x30...0x39).contains($0) }
    guard let first = bytes.first, lowerOrDigit(first), bytes.count <= 81 else { return false }
    return bytes.dropFirst().allSatisfy { lowerOrDigit($0) || $0 == 0x2D }
}

/// The largest downloaded icon accepted, in bytes (Dashboard Icons' WebP files are a few KB).
public let remoteIconMaxBytes = 512 * 1024

/// The Dashboard Icons file on jsDelivr for `slug`; nil for an invalid slug, so nothing but a
/// public icon name ever ends up in the request.
public func remoteIconURL(slug: String) -> URL? {
    guard isValidIconSlug(slug) else { return nil }
    return URL(string: "https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons/webp/\(slug).webp")
}

/// Larger than this decoded is not an icon (the Go core caps inline icons the same).
public let customIconMaxBytes = 64 * 1024

/// A resource's own icon (go/ichorgo/customicon.go): a data: URI's image bytes, or an https URL
/// with a host and no credentials when remoteIcons allows downloads; nil otherwise. It comes from
/// the cluster through the Go core, so it is checked again before it is decoded or requested.
public func customIconSource(_ value: String, remoteIcons: Bool) -> AppIconSource? {
    for type in ["png", "jpeg", "webp", "gif"] {
        let header = "data:image/\(type);base64,"
        guard value.hasPrefix(header) else { continue }
        guard let data = Data(base64Encoded: String(value.dropFirst(header.count))),
              !data.isEmpty, data.count <= customIconMaxBytes else { return nil }
        return .inline(data)
    }
    guard remoteIcons, value.count <= 2048, value.hasPrefix("https://"),
          let components = URLComponents(string: value), components.host?.isEmpty == false,
          components.user == nil, components.password == nil, let url = components.url else { return nil }
    return .url(url)
}

/// Up to two initials: the first letters of the first two words, else the first two letters
/// (same rules as the Android app).
public func monogramInitials(_ name: String) -> String {
    let words = name.split { " -_./".contains($0) }
        .map { $0.filter { $0.isLetter || $0.isNumber } }
        .filter { !$0.isEmpty }
    let initials = words.count >= 2 ? String(words[0].prefix(1) + words[1].prefix(1)) : String(words.first?.prefix(2) ?? "")
    return initials.isEmpty ? "?" : initials.uppercased()
}

/// The monogram's hue in degrees (0..<360), stable for an app id and the same as on Android:
/// FNV-1a of its UTF-8 bytes.
public func monogramHue(_ id: String) -> Int {
    let hash = id.utf8.reduce(UInt32(0x811C_9DC5)) { ($0 ^ UInt32($1)) &* 0x0100_0193 }
    return Int(hash % 360)
}
