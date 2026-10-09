import SwiftUI
import IchorCore

/// What a source sheet sets up: the metrics' query API or the Alertmanager, which differ only in wording.
enum SourceSheetKind {
    case metrics, alertmanager

    var title: String {
        switch self {
        case .metrics: String(localized: "Metrics source")
        case .alertmanager: String(localized: "Alertmanager source")
        }
    }

    var notFound: String {
        switch self {
        case .metrics: String(localized: "No Prometheus, Mimir, Thanos or VictoriaMetrics found in the cluster")
        case .alertmanager: String(localized: "No Alertmanager found in the cluster")
        }
    }

    var pathPrefixHint: String {
        switch self {
        case .metrics: String(localized: "Path prefix (Mimir: /prometheus)")
        case .alertmanager: String(localized: "Path prefix (Mimir: /alertmanager)")
        }
    }

    var urlHint: String {
        switch self {
        case .metrics: String(localized: "URL (e.g. https://mimir.example.com/prometheus)")
        case .alertmanager: String(localized: "URL (e.g. https://alerts.example.com)")
        }
    }

    var answers: String {
        switch self {
        case .metrics: String(localized: "The query API answers.")
        case .alertmanager: String(localized: "The Alertmanager answers.")
        }
    }
}

/// Picks where queries go: a query API (or an Alertmanager) found in the cluster, a Service typed
/// in (both through the Kubernetes API), or a URL with its credentials. Go checks it before it is saved.
struct SourceSheet: View {
    var kind = SourceSheetKind.metrics
    let current: PromSource?
    let discovered: [PromSource]?
    let discovering: Bool
    let discoveryError: String?
    let discover: () async -> Void
    /// Nil when the query API answers, else why not.
    let test: (PromSource) async -> String?
    /// Nil when saved, else why not.
    let save: (PromSource) async -> String?

    @Environment(\.dismiss) private var dismiss
    @State private var source = PromSource()
    @State private var port = ""
    @State private var busy = false
    @State private var outcome: Result<Void, TalosError>?

    private var edited: PromSource {
        var s = source
        if s.mode == PromSource.proxy { s.port = Int(port) ?? 0 }
        return s
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Picker("Mode", selection: Binding(get: { source.mode }, set: { source = source.switched(to: $0); outcome = nil })) {
                        Text("In the cluster").tag(PromSource.proxy)
                        Text("URL").tag(PromSource.url)
                    }
                    .pickerStyle(.segmented)
                }
                if source.mode == PromSource.proxy {
                    discoveredSection
                    Section {
                        TextField("Namespace", text: $source.namespace)
                        TextField("Service", text: $source.service)
                        TextField("Port", text: $port).keyboardType(.numberPad)
                        TextField(kind.pathPrefixHint, text: $source.pathPrefix)
                    } footer: {
                        Text("Reached through the Kubernetes API service proxy with the cluster's admin kubeconfig: no extra credentials.")
                    }
                } else {
                    urlSection
                }
                Section {
                    TextField("Tenant (X-Scope-OrgID, optional)", text: $source.tenant)
                }
                if busy {
                    ProgressView()
                } else if let outcome {
                    switch outcome {
                    case .success: Text(kind.answers).foregroundStyle(.green)
                    case .failure(let error): Text(error.message).foregroundStyle(.red).font(.footnote)
                    }
                }
            }
            .textInputAutocapitalization(.never)
            .autocorrectionDisabled()
            .navigationTitle(kind.title)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItemGroup(placement: .confirmationAction) {
                    Button("Test") { run(test) }.disabled(busy)
                    Button("Save") { run(save, then: { dismiss() }) }.disabled(busy)
                }
            }
            .onAppear {
                source = current ?? discovered?.first ?? PromSource()
                port = source.port > 0 ? String(source.port) : ""
            }
        }
    }

    @ViewBuilder private var discoveredSection: some View {
        Section {
            if discovering { ProgressView() }
            if let discoveryError { Text(discoveryError).foregroundStyle(.red).font(.footnote) }
            if discovered?.isEmpty == true { Text(kind.notFound).foregroundStyle(.secondary) }
            ForEach(discovered ?? [], id: \.self) { found in
                Button {
                    var picked = found
                    picked.tenant = source.tenant
                    source = picked
                    port = String(found.port)
                    outcome = nil
                } label: {
                    HStack {
                        VStack(alignment: .leading) {
                            Text(verbatim: found.label)
                            if !found.kind.isEmpty { Text(verbatim: found.kind).font(.caption).foregroundStyle(.secondary) }
                        }
                        Spacer()
                        if found.sameEndpoint(as: edited) { Image(systemName: "checkmark").foregroundStyle(.tint) }
                    }
                }
                .foregroundStyle(.primary)
            }
            Button("Search the cluster again") { Task { await discover() } }.disabled(discovering)
        }
    }

    @ViewBuilder private var urlSection: some View {
        Section {
            TextField(kind.urlHint, text: $source.url).keyboardType(.URL)
            Picker("Authentication", selection: $source.auth) {
                Text("None").tag(PromSource.authNone)
                Text("Token").tag(PromSource.authBearer)
                Text("Basic").tag(PromSource.authBasic)
            }
            if source.auth == PromSource.authBasic { TextField("User name", text: $source.username) }
            if source.auth == PromSource.authBearer { SecureField("Bearer token", text: $source.secret) }
            if source.auth == PromSource.authBasic { SecureField("Password", text: $source.secret) }
        } footer: {
            if source.auth != PromSource.authNone {
                Text("Credentials are only sent over https, and stay encrypted on this phone.")
            }
        }
        Section {
            TextField("Extra certificate authority (PEM, optional)", text: $source.ca, axis: .vertical).lineLimit(2...6)
            Toggle("Skip certificate verification", isOn: $source.insecureSkipVerify)
        }
    }

    private func run(_ action: @escaping (PromSource) async -> String?, then: @escaping () -> Void = {}) {
        busy = true
        let candidate = edited
        Task {
            let error = await action(candidate)
            busy = false
            outcome = error.map { .failure(TalosError(message: $0)) } ?? .success(())
            if error == nil { then() }
        }
    }
}

/// Adds (empty `initial` id) or edits a panel, from a preset or plain PromQL, with a preview run.
/// With `chat` (AI on), "Ask AI" opens the panel assistant about the draft, which it can replace.
struct PanelEditorSheet: View {
    let initial: PromPanel
    let presets: [PromPanel]
    let preview: (PromPanel) async -> Result<PromResult, Error>
    var chat: PanelChatModel? = nil
    var sourceLabel = ""
    /// Opens the assistant's conversation about the draft before the sheet shows it.
    var openChat: (PromPanel) -> Void = { _ in }
    let onSave: (PromPanel) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var panel = PromPanel()
    @State private var running = false
    @State private var result: Result<PromResult, Error>?
    @State private var chatOpen = false

    var body: some View {
        NavigationStack {
            Form {
                if initial.id.isEmpty && !presets.isEmpty {
                    Section {
                        Menu("Start from a preset") {
                            ForEach(presets) { preset in
                                Button(preset.title) {
                                    panel = PromPanel(title: preset.title, query: preset.query, unit: preset.unit, legend: preset.legend)
                                    result = nil
                                }
                            }
                        }
                    }
                }
                Section {
                    TextField("Title", text: $panel.title)
                    TextField("PromQL query", text: $panel.query, axis: .vertical)
                        .lineLimit(3...8)
                        .font(.body.monospaced())
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                    Picker("Unit", selection: $panel.unit) {
                        ForEach(PromPanel.units, id: \.self) { Text(unitLabel($0)).tag($0) }
                    }
                }
                Section {
                    TextField("Legend", text: $panel.legend)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                } footer: {
                    Text("Labels in {{ }}, e.g. {{namespace}}/{{pod}}. Empty: the series name.")
                }
                Section {
                    Button("Run query") {
                        running = true
                        let candidate = panel
                        Task {
                            result = await preview(candidate)
                            running = false
                        }
                    }
                    .disabled(panel.query.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || running)
                    if chat != nil {
                        Button { openChat(panel); chatOpen = true } label: { Label("Ask AI", systemImage: "sparkles") }
                    }
                    if running { ProgressView() }
                    switch result {
                    case .success(let res): PromChartView(panel: panel, result: res)
                    case .failure(let error): Text(error.localizedDescription).foregroundStyle(.red).font(.footnote)
                    case nil: EmptyView()
                    }
                }
            }
            .navigationTitle(initial.id.isEmpty ? String(localized: "Add panel") : String(localized: "Edit panel"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") {
                        var saved = panel
                        saved.id = initial.id
                        saved.title = saved.title.trimmingCharacters(in: .whitespacesAndNewlines)
                        saved.query = saved.query.trimmingCharacters(in: .whitespacesAndNewlines)
                        saved.legend = saved.legend.trimmingCharacters(in: .whitespacesAndNewlines)
                        onSave(saved)
                        dismiss()
                    }
                    .disabled(panel.title.trimmingCharacters(in: .whitespaces).isEmpty || panel.query.trimmingCharacters(in: .whitespaces).isEmpty)
                }
            }
            .onAppear { panel = initial }
            .sheet(isPresented: $chatOpen) {
                if let chat {
                    // A proposed panel replaces the draft (its id kept); a preview was about the old query.
                    PanelChatSheet(model: chat, sourceLabel: sourceLabel) { proposed in
                        panel = proposed.panel(id: initial.id)
                        result = nil
                        chatOpen = false
                    }
                }
            }
        }
    }

    private func unitLabel(_ unit: String) -> String { promUnitLabel(unit) }
}

/// A panel unit as the user reads it.
func promUnitLabel(_ unit: String) -> String {
    switch unit {
    case "percent": String(localized: "Percent")
    case "bytes": String(localized: "Bytes")
    case "cores": String(localized: "CPU cores")
    case "persec": String(localized: "Per second")
    case "count": String(localized: "Count")
    default: String(localized: "Plain number")
    }
}
