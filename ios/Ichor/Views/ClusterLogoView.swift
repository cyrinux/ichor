import SwiftUI
import IchorCore

/// A cluster's logo (Talos, Kubernetes or its cloud, see clusterLogo) in a ring of the
/// cluster's color; `selected` adds a check badge in that color. Same as Android's ClusterLogo.
struct ClusterLogoView: View {
    let context: ContextSummary
    let color: Color
    let selected: Bool
    var size: CGFloat = 32

    @Environment(\.colorScheme) private var scheme

    var body: some View {
        ZStack(alignment: .bottomTrailing) {
            Circle()
                .strokeBorder(color, lineWidth: 2)
                .overlay {
                    if let logo = BundledAppIcons.image(clusterLogo(context), dark: scheme == .dark) {
                        Image(uiImage: logo)
                            .resizable()
                            .interpolation(.high)
                            .scaledToFit()
                            .padding(size * 0.2)
                    }
                }
                .frame(width: size, height: size)
            if selected {
                Image(systemName: "checkmark")
                    .font(.system(size: size * 0.24, weight: .bold))
                    .foregroundStyle(.white)
                    .frame(width: size * 0.45, height: size * 0.45)
                    .background(color, in: Circle())
                    .overlay(Circle().strokeBorder(Color(.systemBackground), lineWidth: 1.5))
            }
        }
        .frame(width: size, height: size)
        .accessibilityHidden(true)
    }
}
