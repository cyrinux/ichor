import SwiftUI
import IchorCore

/// The outcome of testing an endpoint: under way, what it answered, or why it did not.
private enum ProbeResult {
    case running
    case answered(EndpointProbe)
    case failed(String)
}

/// Edits the endpoints of `context`'s talosconfig context (the addresses the app connects
/// through): add, remove, test each with the cluster's credentials. An endpoint that does not
/// answer now can still be saved (it may answer over a VPN). `onScan` searches the network instead.
struct EndpointsSheet: View {
    let context: ContextSummary
    let onScan: () -> Void

    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var endpoints: [String]
    @State private var probes: [String: ProbeResult] = [:]
    @State private var draft = ""
    @State private var saving = false
    @State private var error: String?

    init(context: ContextSummary, onScan: @escaping () -> Void) {
        self.context = context
        self.onScan = onScan
        _endpoints = State(initialValue: context.endpoints)
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    ForEach(endpoints, id: \.self) { endpoint in
                        row(endpoint)
                    }
                    .onDelete { endpoints.remove(atOffsets: $0) }
                } footer: {
                    Text("The addresses the app reaches the Talos API through (port 50000 unless given, e.g. 192.168.1.10:50001). Test one with the cluster's credentials; one that does not answer now can still be saved, it may answer over a VPN.")
                }
                Section {
                    HStack {
                        TextField("Add an endpoint", text: $draft, prompt: Text(verbatim: "192.168.1.10"))
                            .keyboardType(.URL)
                            .textInputAutocapitalization(.never)
                            .autocorrectionDisabled()
                            .font(.body.monospaced())
                            .onSubmit(add)
                            .onChange(of: draft) { _, text in if text.count > endpointMax { draft = String(text.prefix(endpointMax)) } }
                        Button(action: add) { Image(systemName: "plus.circle.fill") }
                            .buttonStyle(.borderless)
                            .disabled(!isEndpoint(trimmedDraft) || endpoints.contains(trimmedDraft))
                            .accessibilityLabel(Text("Add an endpoint"))
                    }
                }
                Section {
                    Button {
                        dismiss()
                        onScan()
                    } label: {
                        Label("Search the local network", systemImage: "antenna.radiowaves.left.and.right")
                    }
                    .disabled(saving)
                }
                if let error {
                    Section { Text(error).font(.footnote).foregroundStyle(.statusBad) }
                }
            }
            .themedBackground()
            .navigationTitle(String(localized: "Endpoints of \(model.labels.of(context))"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }.disabled(saving)
                }
                ToolbarItem(placement: .confirmationAction) {
                    if saving {
                        ProgressView()
                    } else {
                        Button("Save", action: save).disabled(endpoints.isEmpty || endpoints == context.endpoints)
                    }
                }
            }
            .interactiveDismissDisabled(saving)
        }
    }

    private var trimmedDraft: String { draft.trimmingCharacters(in: .whitespaces) }

    private func row(_ endpoint: String) -> some View {
        HStack {
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: endpoint).font(.body.monospaced()).lineLimit(1).truncationMode(.middle)
                switch probes[endpoint] {
                case .answered(let probe):
                    Text("Answered: \(probe.summary)").font(.caption).foregroundStyle(.statusOK)
                case .failed(let message):
                    Text(message).font(.caption).foregroundStyle(.statusBad)
                case .running, nil:
                    EmptyView()
                }
            }
            Spacer()
            if case .running = probes[endpoint] {
                ProgressView()
            } else {
                Button { probe(endpoint) } label: { Image(systemName: "network") }
                    .buttonStyle(.borderless)
                    .accessibilityLabel(Text("Test \(endpoint)"))
            }
            Button { endpoints.removeAll { $0 == endpoint } } label: {
                Image(systemName: "minus.circle").foregroundStyle(.red)
            }
            .buttonStyle(.borderless)
            .accessibilityLabel(Text("Remove \(endpoint)"))
        }
    }

    private func add() {
        let endpoint = trimmedDraft
        guard isEndpoint(endpoint), !endpoints.contains(endpoint) else { return }
        endpoints.append(endpoint)
        draft = ""
        probe(endpoint)
    }

    private func probe(_ endpoint: String) {
        guard let yaml = model.yaml else { return }
        probes[endpoint] = .running
        let name = context.name
        Task {
            do {
                probes[endpoint] = .answered(try await TalosClient.probeEndpoint(stored: yaml, context: name, endpoint: endpoint))
            } catch {
                probes[endpoint] = .failed(error.localizedDescription)
            }
        }
    }

    private func save() {
        saving = true
        error = nil
        Task {
            defer { saving = false }
            do {
                try await model.setEndpoints(endpoints, of: context.name)
                dismiss()
            } catch {
                self.error = error.localizedDescription
            }
        }
    }
}
