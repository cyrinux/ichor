import SwiftUI
import TalosViewerCore

enum LoadState<T> {
    case loading
    case loaded(T)
    case failed(String)
}

extension LoadState {
    static func from(_ operation: () async throws -> T) async -> LoadState<T> {
        do { return .loaded(try await operation()) } catch { return .failed(error.localizedDescription) }
    }
}

/// Spinner / error with retry / content for a LoadState.
struct LoadStateView<T, Content: View>: View {
    let state: LoadState<T>
    let retry: () async -> Void
    @ViewBuilder let content: (T) -> Content

    var body: some View {
        switch state {
        case .loading:
            ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
        case .failed(let message):
            ContentUnavailableView {
                Label("Request failed", systemImage: "exclamationmark.triangle")
            } description: {
                Text(message)
            } actions: {
                Button("Retry") { Task { await retry() } }
            }
        case .loaded(let value):
            content(value)
        }
    }
}

struct StatusPill: View {
    let label: String
    let color: Color

    var body: some View {
        HStack(spacing: 6) {
            Circle().fill(color).frame(width: 8, height: 8)
            Text(label).font(.caption.weight(.medium))
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 4)
        .foregroundStyle(color)
        .background(color.opacity(0.15), in: Capsule())
    }
}

extension NodeHealth {
    var label: String {
        switch self {
        case .ready: "Ready"
        case .notReady: "Not ready"
        case .unreachable: "Unreachable"
        }
    }

    var color: Color {
        switch self {
        case .ready: .green
        case .notReady: .orange
        case .unreachable: .red
        }
    }
}

struct UsageBar: View {
    let fraction: Double

    var body: some View {
        ProgressView(value: min(max(fraction, 0), 1))
            .tint(fraction >= 0.9 ? .red : fraction >= 0.75 ? .orange : .green)
    }
}

struct RoleNotice: View {
    let feature: Feature
    let roles: [String]

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text("\(feature.label) needs \(feature.minimumRole)").font(.headline)
            Text("This talosconfig has \(roles.isEmpty ? "no roles" : roles.joined(separator: ", ")). Import a talosconfig created with --roles \(feature.minimumRole) to use it.")
                .font(.footnote)
                .foregroundStyle(.secondary)
        }
        .padding()
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.quaternary, in: RoundedRectangle(cornerRadius: 12))
    }
}

/// "True black" theme: pure black list backgrounds for OLED screens.
struct ThemedBackground: ViewModifier {
    @Environment(AppModel.self) private var model

    func body(content: Content) -> some View {
        if model.theme == .black {
            content.scrollContentBackground(.hidden).background(Color.black)
        } else {
            content
        }
    }
}

extension View {
    func themedBackground() -> some View { modifier(ThemedBackground()) }
}
