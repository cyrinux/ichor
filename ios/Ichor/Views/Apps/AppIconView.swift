import SwiftUI
import IchorCore

/// An app's icon on a rounded tile: the resource's own icon (inline, or downloaded when allowed),
/// the bundled logo (its light variant in dark mode), the downloaded one when the user allowed it, else a monogram (or fallbackSymbol, an SF Symbol,
/// when given). The tile's subtle fill keeps transparent logos readable in both themes.
struct AppIconView: View {
    let app: InventoryApp
    var size: CGFloat = 52
    var fallbackSymbol: String?

    @Environment(\.colorScheme) private var scheme
    @AppStorage(AppIconSettings.remoteKey) private var remoteIcons = false
    @State private var downloaded: UIImage?

    var body: some View {
        let source = app.iconSource(remoteIcons: remoteIcons)
        Group {
            if let image = image(for: source) {
                Image(uiImage: image)
                    .resizable()
                    .interpolation(.high)
                    .scaledToFit()
                    .padding(size * 0.16)
            } else if let fallbackSymbol {
                RoundedRectangle(cornerRadius: size * 0.3, style: .continuous)
                    .fill(Color.accentColor.opacity(0.18))
                    .overlay {
                        Image(systemName: fallbackSymbol)
                            .font(.system(size: size * 0.46, weight: .medium))
                            .foregroundStyle(Color.accentColor)
                    }
            } else {
                MonogramView(app: app, size: size)
            }
        }
        .frame(width: size, height: size)
        .background(Color(.tertiarySystemFill), in: RoundedRectangle(cornerRadius: size * 0.3, style: .continuous))
        .accessibilityHidden(true)
        .task(id: source) {
            downloaded = await Self.load(source)
        }
    }

    private func image(for source: AppIconSource) -> UIImage? {
        switch source {
        case .bundled(let name): BundledAppIcons.image(name, dark: scheme == .dark)
        case .remote, .url, .inline: downloaded
        case .monogram: nil
        }
    }

    /// Icons that are not bundled: downloaded, or decoded from the resource's inline bytes.
    private static func load(_ source: AppIconSource) async -> UIImage? {
        switch source {
        case .remote(let slug): return await RemoteAppIcons.shared.image(slug: slug)
        case .url(let url): return await RemoteAppIcons.shared.image(url: url)
        case .inline(let data): return await RemoteAppIcons.shared.image(inline: data)
        case .bundled, .monogram: return nil
        }
    }
}

/// Up to two initials on a square colored from the app id: the Android app's colors, a deep
/// tone with light text in dark mode, a pale one with dark text in light mode.
private struct MonogramView: View {
    let app: InventoryApp
    let size: CGFloat

    @Environment(\.colorScheme) private var scheme

    var body: some View {
        let hue = Double(monogramHue(app.id)) / 360
        let dark = scheme == .dark
        RoundedRectangle(cornerRadius: size * 0.3, style: .continuous)
            .fill(hsl(hue, dark ? 0.42 : 0.55, dark ? 0.30 : 0.86))
            .overlay {
                Text(verbatim: monogramInitials(app.name))
                    .font(.system(size: size * 0.36, weight: .semibold))
                    .foregroundStyle(hsl(hue, dark ? 0.70 : 0.60, dark ? 0.86 : 0.26))
                    .lineLimit(1)
                    .minimumScaleFactor(0.5)
            }
    }

    /// HSL (like Compose's Color.hsl) as SwiftUI's HSB.
    private func hsl(_ hue: Double, _ saturation: Double, _ lightness: Double) -> Color {
        let brightness = lightness + saturation * min(lightness, 1 - lightness)
        let hsbSaturation = brightness == 0 ? 0 : 2 * (1 - lightness / brightness)
        return Color(hue: hue, saturation: hsbSaturation, brightness: brightness)
    }
}
