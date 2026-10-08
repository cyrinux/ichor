import SwiftUI
import IchorCore

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

/// How a home's sections are named in the editor: a title and one line on what the section shows.
protocol CardLook {
    var title: Text { get }
    var detail: Text { get }
}

extension OverviewCard: CardLook {}

/// Arranges a home screen (Android's HomeEditor), the Talos overview's or the Kubernetes home's:
/// which toolbar actions are icons and which are in the ⋯ menu, and the sections' order, hidden
/// ones shown again at the end. Every change is saved at once, for every cluster. Sections the
/// cluster lacks (`absent`: no Argo CD, no Flux...) are not offered.
struct HomeEditorSheet<Card: HomeCard & CardLook, Action: BarAction & BarActionLook>: View {
    let title: LocalizedStringKey
    var absent: Set<Card> = []
    @AppStorage private var layoutText: String
    @AppStorage private var barText: String
    @Environment(\.dismiss) private var dismiss

    init(_ card: Card.Type, _ action: Action.Type, title: LocalizedStringKey, absent: Set<Card> = []) {
        self.title = title
        self.absent = absent
        _layoutText = AppStorage(wrappedValue: "", CardLayout<Card>.storageKey)
        _barText = AppStorage(wrappedValue: "", ActionBar<Action>.storageKey)
    }

    private var layout: CardLayout<Card> { .parse(layoutText) }

    var body: some View {
        NavigationStack {
            List {
                ActionBarSection<Action>(text: $barText)
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
                    Section { Button("Reset to default") { save(CardLayout<Card>()) } }
                }
            }
            // Always arranging: the handles are what this sheet is for.
            .environment(\.editMode, .constant(.active))
            .navigationTitle(title)
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

    private func save(_ layout: CardLayout<Card>) {
        withAnimation { layoutText = layout.encoded }
    }
}

/// The Talos overview's editor.
typealias OverviewEditorSheet = HomeEditorSheet<OverviewCard, OverviewAction>

private struct CardName<Card: HomeCard & CardLook>: View {
    let card: Card

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
