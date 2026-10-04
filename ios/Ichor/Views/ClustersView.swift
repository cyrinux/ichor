import SwiftUI
import IchorCore
import UIKit

extension UIColor {
    /// From 0xRRGGBB.
    convenience init(rgb: Int) {
        self.init(
            red: CGFloat(rgb >> 16 & 0xFF) / 255,
            green: CGFloat(rgb >> 8 & 0xFF) / 255,
            blue: CGFloat(rgb & 0xFF) / 255,
            alpha: 1
        )
    }

    /// As 0xRRGGBB (sRGB, alpha dropped).
    var rgb: Int {
        var red: CGFloat = 0, green: CGFloat = 0, blue: CGFloat = 0, alpha: CGFloat = 0
        getRed(&red, green: &green, blue: &blue, alpha: &alpha)
        func channel(_ value: CGFloat) -> Int { Int((min(max(value, 0), 1) * 255).rounded()) }
        return channel(red) << 16 | channel(green) << 8 | channel(blue)
    }
}

/// A cluster's main color (0xRRGGBB) as an accent: its hue at the tone that reads on the
/// light and on the dark backgrounds (the tones of Android's primary color).
func clusterAccent(_ seed: Int) -> Color {
    Color(uiColor: UIColor { traits in
        UIColor(rgb: tonalColor(seed: seed, tone: traits.userInterfaceStyle == .dark ? darkAccentTone : lightAccentTone))
    })
}

extension AppModel {
    /// The app's accent color: the one of the cluster on screen.
    var accent: Color { clusterAccent(seed(of: activeSummary)) }
}

/// Under the overview's title when several clusters are imported: a dot per cluster, in its
/// color. Swipe it sideways for the previous or next cluster (left: the next one), tap it
/// for the list.
struct ClusterBar: View {
    @Environment(AppModel.self) private var model
    let onOpen: () -> Void

    private static let swipeDistance: CGFloat = 48

    var body: some View {
        HStack(spacing: 8) {
            ForEach(model.summary?.contexts ?? []) { context in
                let active = context.name == model.activeContext
                Circle()
                    .fill(clusterAccent(model.seed(of: context)))
                    .opacity(active ? 1 : 0.5)
                    .frame(width: active ? 10 : 7, height: active ? 10 : 7)
            }
        }
        // 44 pt: the minimum touch target; the dots stay small.
        .frame(maxWidth: .infinity, minHeight: 44)
        .background(.bar)
        .contentShape(Rectangle())
        .onTapGesture(perform: onOpen)
        .gesture(DragGesture(minimumDistance: 24).onEnded { value in
            let distance = value.translation.width
            guard abs(distance) > abs(value.translation.height), abs(distance) >= Self.swipeDistance else { return }
            model.selectAdjacentCluster(step: distance < 0 ? 1 : -1)
        })
        .animation(.snappy, value: model.activeContext)
        .accessibilityElement()
        .accessibilityLabel(Text("Switch cluster"))
        .accessibilityValue(Text(model.activeLabel))
        .accessibilityAddTraits(.isButton)
        .accessibilityAdjustableAction { direction in
            model.selectAdjacentCluster(step: direction == .increment ? 1 : -1)
        }
    }
}

/// The imported clusters: pick the one on screen, rename it, give it a color, remove it, add one.
struct ClustersView: View {
    @Environment(AppModel.self) private var model
    @State private var removing: ContextSummary?
    @State private var renaming: ContextSummary?
    @State private var newName = ""
    @State private var error: String?

    var body: some View {
        List {
            Section {
                ForEach(model.summary?.contexts ?? []) { context in
                    row(context)
                }
            } footer: {
                Text("The app takes the colors of the cluster on screen.")
            }
            if let error {
                Section { Text(error).font(.footnote).foregroundStyle(.statusBad) }
            }
            Section {
                NavigationLink(value: Route.importConfig) { Label("Add a cluster", systemImage: "plus") }
            }
        }
        .themedBackground()
        .navigationTitle("Clusters")
        .confirmationDialog(
            removing.map { String(localized: "Remove \(model.labels.of($0))?") } ?? "",
            isPresented: $removing.isPresent(),
            titleVisibility: .visible,
            presenting: removing
        ) { context in
            Button("Delete", role: .destructive) { remove(context) }
        } message: { _ in
            Text("This cluster's config and client key will be removed from this device. The other clusters are kept.")
        }
        .alert(
            renaming.map { String(localized: "Rename \(model.labels.of($0))") } ?? "",
            isPresented: $renaming.isPresent(),
            presenting: renaming
        ) { context in
            TextField("Name", text: $newName, prompt: Text(verbatim: context.name))
            Button("OK") { model.rename(context, to: newName) }
            Button("Cancel", role: .cancel) {}
        } message: { context in
            Text("Shown on this device only, the talosconfig keeps \(context.name). Leave empty to use it again.")
        }
    }

    private func startRenaming(_ context: ContextSummary) {
        newName = model.labels.given(context) ?? ""
        renaming = context
    }

    private func row(_ context: ContextSummary) -> some View {
        let active = context.name == model.activeContext
        return HStack {
            Button { model.activeContext = context.name } label: {
                HStack {
                    Image(systemName: active ? "checkmark.circle.fill" : "circle")
                        .foregroundStyle(active ? Color.accentColor : Color.secondary)
                        .accessibilityHidden(true)
                    VStack(alignment: .leading) {
                        Text(model.labels.of(context)).foregroundStyle(Color.primary)
                        // Renamed: which talosconfig context that is.
                        if model.labels.given(context) != nil {
                            Text(context.name).font(.caption).foregroundStyle(Color.secondary)
                        }
                        Text([context.endpoints.first, context.localizedAccessLabel].compactMap { $0 }.joined(separator: " · "))
                            .font(.caption)
                            .foregroundStyle(Color.secondary)
                    }
                    Spacer()
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityAddTraits(active ? .isSelected : [])
            ColorPicker(String(localized: "Color of \(model.labels.of(context))"), selection: color(of: context), supportsOpacity: false)
                .labelsHidden()
        }
        .swipeActions(edge: .trailing) {
            // Not a destructive-role button: the row must stay until the removal is confirmed.
            Button { removing = context } label: { Label("Delete", systemImage: "trash") }
                .tint(.red)
            // Not in the screenshot mode: the field would show the given name.
            if !model.labels.masked && !context.fingerprint.isEmpty {
                Button { startRenaming(context) } label: { Label("Rename", systemImage: "pencil") }
            }
        }
    }

    private func color(of context: ContextSummary) -> Binding<Color> {
        Binding(
            get: { Color(uiColor: UIColor(rgb: model.seed(of: context))) },
            set: { model.setColor(UIColor($0).rgb, for: context) }
        )
    }

    private func remove(_ context: ContextSummary) {
        Task {
            do {
                try await model.removeCluster(context.name)
                error = nil
            } catch {
                self.error = error.localizedDescription
            }
        }
    }
}
