import SwiftUI
import IchorCore

extension OverviewAction: BarActionLook {
    var systemImage: String {
        switch self {
        case .health: "heart.text.square"
        case .events: "list.bullet.rectangle"
        case .workloads: "square.stack.3d.up"
        case .metrics: "chart.xyaxis.line"
        case .kubespan: "point.3.connected.trianglepath.dotted"
        case .etcd: "cylinder.split.1x2"
        case .settings: "gearshape"
        }
    }

    var title: Text {
        switch self {
        case .health: Text("Cluster health")
        case .events: Text("Events")
        case .workloads: Text("Kubernetes workloads")
        case .metrics: Text("Metrics")
        case .kubespan: Text(verbatim: "KubeSpan")
        case .etcd: Text(verbatim: "etcd")
        case .settings: Text("Settings")
        }
    }
}

extension OverviewCard {
    var title: Text {
        switch self {
        case .talosUpdate: Text("Talos update")
        case .summary: Text("Cluster summary")
        case .apps: Text("Apps")
        case .dataServices: Text("Data services")
        case .argoCD: Text(verbatim: "Argo CD")
        case .flux: Text(verbatim: "Flux")
        case .nodes: Text("Nodes")
        case .timeDrift: Text("Clock drift")
        }
    }

    /// One line on what the section shows, so the editor's names need no guessing.
    var detail: Text {
        switch self {
        case .talosUpdate: Text("A newer Talos release than the nodes run, when there is one")
        case .summary: Text("Nodes, Talos version, CPU and memory at a glance")
        case .apps: Text("Apps running on the cluster and those that need a look")
        case .dataServices: Text("Health of Longhorn, Garage and CloudNativePG")
        case .argoCD: Text("Sync and health of the Argo CD apps, syncs in progress")
        case .flux: Text("Ready, reconciling and failing Kustomizations and HelmReleases")
        case .nodes: Text("Every node with its state")
        case .timeDrift: Text("Clock offset of each node against NTP")
        }
    }
}

/// Arranges the overview (Android's OverviewEditor): which toolbar actions are icons and which
/// are in the ⋯ menu, and the sections' order, hidden ones shown again at the end. Every change
/// is saved at once, for every cluster. Sections the cluster lacks (`absent`: no Argo CD, no
/// Flux...) are not offered.
struct OverviewEditorSheet: View {
    var absent: Set<OverviewCard> = []
    @AppStorage(OverviewLayout.storageKey) private var layoutText = ""
    @AppStorage(OverviewBar.storageKey) private var barText = ""
    @Environment(\.dismiss) private var dismiss

    private var layout: OverviewLayout { .parse(layoutText) }

    var body: some View {
        NavigationStack {
            List {
                ActionBarSection<OverviewAction>(text: $barText)
                cardsSection
                if !layout.hiddenCards(absent: absent).isEmpty {
                    Section("Hidden cards") {
                        ForEach(layout.hiddenCards(absent: absent)) { card in
                            HStack {
                                CardName(card: card).opacity(0.6)
                                Spacer()
                                Button { save(layout.showing(card)) } label: {
                                    Image(systemName: "plus.circle.fill").foregroundStyle(.green)
                                }
                                .buttonStyle(.borderless)
                                .accessibilityLabel(Text("Show"))
                            }
                        }
                    }
                }
                if !layout.isDefault {
                    Section { Button("Reset to default") { save(OverviewLayout()) } }
                }
            }
            // Always arranging: the handles are what this sheet is for.
            .environment(\.editMode, .constant(.active))
            .navigationTitle("Customize overview")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } }
            }
        }
    }

    private var cardsSection: some View {
        Section {
            ForEach(layout.visible(absent: absent)) { card in
                HStack {
                    CardName(card: card)
                    Spacer()
                    Button { save(layout.hiding(card)) } label: {
                        Image(systemName: "minus.circle.fill").foregroundStyle(.red)
                    }
                    .buttonStyle(.borderless)
                    .accessibilityLabel(Text("Hide"))
                }
            }
            .onMove { source, destination in save(layout.moving(fromOffsets: source, toOffset: destination, absent: absent)) }
        } header: {
            Text("Cards")
        } footer: {
            Text("Drag a card by its handle to move it. Hidden cards can be added back below.")
        }
    }

    private func save(_ layout: OverviewLayout) {
        withAnimation { layoutText = layout.encoded }
    }
}

private struct CardName: View {
    let card: OverviewCard

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            card.title
            card.detail.font(.caption).foregroundStyle(.secondary)
            if card.whenDetected {
                Text("Only when found on the cluster").font(.caption).foregroundStyle(.secondary)
            }
        }
    }
}
