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
    @State private var editingEndpoints: ContextSummary?
    @State private var scanning = false
    /// The sign-in of each kubeconfig cluster that signs in through a method, by fingerprint.
    @State private var signIns: [String: KubeSignInInfo] = [:]
    @State private var signingIn: KubeSignInTarget?

    var body: some View {
        List {
            Section {
                ForEach(model.summary?.contexts ?? []) { context in
                    row(context)
                }
            } footer: {
                VStack(alignment: .leading, spacing: 6) {
                    Text("The app takes the colors of the cluster on screen.")
                    // What the lock on a row means, once there is one.
                    if model.summary?.contexts.contains(where: { model.vpnOnly.contains($0.fingerprint) }) == true {
                        Text("VPN only: without a VPN connected the app does not try the cluster, and background checks wait for it.")
                    }
                    Text("Touch and hold a cluster for its endpoints and the VPN only setting.")
                }
            }
            if let error {
                Section { Text(error).font(.footnote).foregroundStyle(.statusBad) }
            }
            Section {
                NavigationLink(value: Route.importConfig) { Label("Add a cluster", systemImage: "plus") }
                // It looks for Talos endpoints, with the talosconfig's credentials.
                if model.yaml != nil {
                    Button { scanning = true } label: {
                        Label("Search the local network", systemImage: "antenna.radiowaves.left.and.right")
                    }
                }
            }
        }
        .endpointTools(editing: $editingEndpoints, scanning: $scanning)
        .sheet(item: $signingIn) { KubeSignInSheet(target: $0) }
        .task(id: "\(model.summary?.contexts.map(\.fingerprint) ?? [])#\(model.dataGeneration)") { await loadSignIns() }
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
            TextField("Name", text: $newName, prompt: Text(verbatim: parseCloudContext(context.name)?.cluster ?? context.name))
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
                    ClusterLogoView(context: context, color: color(of: context).wrappedValue, selected: active)
                    VStack(alignment: .leading) {
                        HStack(spacing: 4) {
                            Text(model.labels.of(context)).foregroundStyle(Color.primary)
                            if model.vpnOnly.contains(context.fingerprint) {
                                Image(systemName: "lock.shield")
                                    .font(.caption)
                                    .foregroundStyle(Color.secondary)
                                    .accessibilityLabel(Text("VPN only"))
                            }
                        }
                        let cloud = model.labels.cloud(context)
                        // Renamed: which talosconfig context that is.
                        if model.labels.given(context) != nil {
                            Text(verbatim: cloud?.cluster ?? context.name).font(.caption).foregroundStyle(Color.secondary)
                        }
                        if let cloud {
                            Text(verbatim: cloud.localizedDetail).font(.caption).foregroundStyle(Color.secondary)
                        }
                        Text([context.endpoints.first, context.localizedAccessLabel].compactMap { $0 }.joined(separator: " · "))
                            .font(.caption)
                            .foregroundStyle(Color.secondary)
                        if let kube = model.kubeAccessTarget(of: context) {
                            Text("Kubernetes through \(model.labels.of(kube))")
                                .font(.caption)
                                .foregroundStyle(Color.secondary)
                        }
                        if let user = signIns[context.fingerprint].flatMap({ $0.signedIn ? $0.user : nil }) {
                            Text("Signed in as \(user)")
                                .font(.caption)
                                .foregroundStyle(Color.secondary)
                        }
                    }
                    Spacer()
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityAddTraits(active ? .isSelected : [])
            if signIns[context.fingerprint]?.signedIn == false, let target = model.signInTarget(for: context) {
                Button("Sign in") { signingIn = target }
                    .buttonStyle(.borderless)
                    .font(.caption)
            }
            ColorPicker(String(localized: "Color of \(model.labels.of(context))"), selection: color(of: context), supportsOpacity: false)
                .labelsHidden()
        }
        .contextMenu { menu(context) }
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

    @ViewBuilder
    private func menu(_ context: ContextSummary) -> some View {
        if !context.fingerprint.isEmpty {
            Toggle(isOn: Binding(get: { model.vpnOnly.contains(context.fingerprint) },
                                 set: { model.setVpnOnly($0, for: context) })) {
                Label("VPN only", systemImage: "lock.shield")
            }
        }
        // Not in screenshot mode (the endpoints shown are fake), nor for the demo; Talos endpoints
        // only (a kubeconfig cluster's server comes with its kubeconfig).
        if !model.labels.masked && !context.demo && !context.isKube {
            Button { editingEndpoints = context } label: { Label("Edit endpoints", systemImage: "point.3.connected.trianglepath.dotted") }
        }
        if !model.labels.masked && !context.fingerprint.isEmpty {
            Button { startRenaming(context) } label: { Label("Rename", systemImage: "pencil") }
        }
        // An Omni cluster's Kubernetes goes through Omni, with its sign-in: no link.
        if !context.isKube && !context.demo && !context.omni && !context.fingerprint.isEmpty && !model.kubeContexts.isEmpty {
            kubeAccessPicker(context)
        }
        if context.isKube || context.omni, let target = model.signInTarget(for: context) {
            Button { signingIn = target } label: { Label("Sign in", systemImage: "person.badge.key") }
        }
        Button(role: .destructive) { removing = context } label: { Label("Delete", systemImage: "trash") }
    }

    /// A Talos cluster's Kubernetes access: the admin kubeconfig Talos issues, or a kubeconfig
    /// cluster (its own identity and RBAC, e.g. an OIDC sign-in).
    private func kubeAccessPicker(_ context: ContextSummary) -> some View {
        Picker(selection: Binding(
            get: { model.kubeAccessTarget(of: context)?.fingerprint ?? "" },
            set: { fingerprint in model.setKubeAccess(model.kubeContexts.first { $0.fingerprint == fingerprint }, for: context) }
        )) {
            Text("Talos admin kubeconfig").tag("")
            ForEach(model.kubeContexts) { kube in
                Text(model.labels.of(kube)).tag(kube.fingerprint)
            }
        } label: {
            Label("Kubernetes access", systemImage: "key")
        }
        .pickerStyle(.menu)
    }

    /// Whether each cluster that signs in through a method (kubeconfig, Omni) is signed in.
    private func loadSignIns() async {
        var loaded: [String: KubeSignInInfo] = [:]
        for context in model.summary?.contexts ?? [] where context.signIn != nil {
            guard let target = model.signInTarget(for: context), target.context == context.name else { continue }
            if let info = try? await TalosClient.signInInfo(kube: target.kube, context: target.context, talos: target.talos) {
                loaded[context.fingerprint] = info
            }
        }
        signIns = loaded
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
