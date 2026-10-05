import SwiftUI
import IchorCore

/// Searches the local networks (EndpointScanJob) and shows the outcome. `onEditEndpoints`, when
/// set, is offered once nothing was found.
struct EndpointScanSheet: View {
    let onEditEndpoints: (() -> Void)?

    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    private var job: EndpointScanJob { .shared }

    var body: some View {
        NavigationStack {
            List {
                switch job.state {
                case .idle:
                    Text("Looks for the nodes of your clusters on the networks around the phone.")
                case .scanning(let networks):
                    Section {
                        HStack(spacing: 12) {
                            ProgressView()
                            Text("Looking for Talos nodes…")
                        }
                    } footer: {
                        NetworksSearched(networks: networks)
                    }
                case .done(let matches, let networks):
                    result(matches, networks: networks)
                case .failed(let message):
                    Text(message).foregroundStyle(.statusBad)
                    editButton
                }
            }
            .themedBackground()
            .navigationTitle("Search the local network")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } }
            }
        }
        .task {
            // A result from before (the sheet closed while that search ran) gives way to a new search.
            job.reset()
            if case .idle = job.state { job.start(model: model) }
        }
        .onDisappear { job.reset() }
    }

    @ViewBuilder
    private func result(_ matches: [EndpointMatch], networks: [String]) -> some View {
        if matches.isEmpty {
            Section {
                Text("No node of your clusters answered on these networks. If you know a node's address, add it as an endpoint.")
                LocalNetworkNotice()
            } footer: {
                NetworksSearched(networks: networks)
            }
            editButton
        } else {
            Section {
                ForEach(matches, id: \.self) { match in
                    VStack(alignment: .leading, spacing: 2) {
                        Text(verbatim: [match.endpoint, match.hostname, match.role, match.version].filter { !$0.isEmpty }.joined(separator: " · "))
                            .font(.callout.monospaced())
                        Text("Added to \(match.contexts.map(label).joined(separator: ", "))")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }
            } header: {
                Text("Found \(matches.count) nodes, added as endpoints:")
            } footer: {
                Text("Only nodes that accept the cluster's credentials are added. Once the cluster answers, the overview offers to add its other nodes.")
            }
        }
    }

    @ViewBuilder
    private var editButton: some View {
        if let onEditEndpoints {
            Section {
                Button("Edit endpoints") {
                    dismiss()
                    onEditEndpoints()
                }
            }
        }
    }

    /// A context named as the cluster list does.
    private func label(_ name: String) -> String {
        model.summary?.context(named: name).map(model.labels.of) ?? name
    }
}

private struct NetworksSearched: View {
    let networks: [String]

    var body: some View {
        Text("Networks searched: \(networks.joined(separator: ", "))").font(.caption.monospaced())
    }
}

/// iOS asks once for local network access and offers no way to read the answer: says what
/// a refusal does, with a way to the app's page in the Settings app, where it can be allowed.
struct LocalNetworkNotice: View {
    var centered = false

    var body: some View {
        VStack(alignment: centered ? .center : .leading, spacing: 6) {
            Text("If Ichor was not allowed to reach devices on your local network, nodes on your Wi-Fi cannot answer. Allow it in the Settings app (Ichor › Local Network).")
                .font(.footnote)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(centered ? .center : .leading)
            Button("Open Settings") { LocalNetwork.openSettings() }
                .buttonStyle(.bordered)
                .font(.footnote)
        }
    }
}

/// The endpoint editor of the context in `editing` and the network search (`scanning`), over
/// a screen; each can open the other (Android's EndpointTools).
private struct EndpointTools: ViewModifier {
    @Binding var editing: ContextSummary?
    @Binding var scanning: Bool

    @Environment(AppModel.self) private var model
    /// What to open once the sheet on screen is gone: SwiftUI shows one sheet at a time.
    @State private var next: Next?

    private enum Next { case scan, edit(ContextSummary) }

    func body(content: Content) -> some View {
        content
            .sheet(item: $editing, onDismiss: openNext) { context in
                EndpointsSheet(context: context) { next = .scan }
            }
            .sheet(isPresented: $scanning, onDismiss: openNext) {
                EndpointScanSheet(onEditEndpoints: editableActive.map { context in { next = .edit(context) } })
            }
    }

    /// The cluster on screen, unless its endpoints cannot be edited here: in screenshot mode
    /// the editor would show, and save, fake endpoints; the demo has none.
    private var editableActive: ContextSummary? {
        guard !model.labels.masked, let context = model.activeSummary, !context.demo else { return nil }
        return context
    }

    private func openNext() {
        guard let pending = next else { return }
        next = nil
        switch pending {
        case .scan: scanning = true
        case .edit(let context): editing = context
        }
    }
}

extension View {
    /// Presents the endpoint editor while `editing` is set and the network search while `scanning`.
    func endpointTools(editing: Binding<ContextSummary?>, scanning: Binding<Bool>) -> some View {
        modifier(EndpointTools(editing: editing, scanning: scanning))
    }
}
