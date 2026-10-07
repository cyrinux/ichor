import SwiftUI
import IchorCore
import UniformTypeIdentifiers

/// Import a talosconfig or a kubeconfig from a file, pasted text or a QR code; told apart,
/// validated by the Go core and previewed before it is stored in the Keychain.
struct ImportView: View {
    var onImported: () -> Void

    // Explicit: the private @State properties make the memberwise init private.
    init(onImported: @escaping () -> Void = {}) {
        self.onImported = onImported
    }

    @Environment(AppModel.self) private var model
    @State private var source = Source.file
    @State private var pasted = ""
    @State private var showingImporter = false
    @State private var preview: Preview?
    @State private var error: String?
    @State private var busy = false
    @State private var showingHelp = false
    @State private var showingDiscovery = false
    /// The credentials cloud discovery found the previewed clusters with: the added ones that
    /// sign in with credentials are signed in with them.
    @State private var discovered: DiscoveredClusters?

    /// A validated config waiting for the user's go: a talosconfig, or a kubeconfig with the
    /// stored clusters its contexts are named like.
    enum Preview {
        case talos(yaml: String, summary: ConfigSummary)
        case kube(yaml: String, summary: ConfigSummary, conflicts: [KubeImportConflict])
    }

    enum Source: String, CaseIterable, Identifiable {
        case file = "File", paste = "Paste", qr = "QR code"
        var id: String { rawValue }

        var label: String {
            switch self {
            case .file: String(localized: "File")
            case .paste: String(localized: "Paste")
            case .qr: String(localized: "QR code")
            }
        }
    }

    var body: some View {
        Group {
            switch preview {
            case .talos(let yaml, let summary)?:
                PreviewList(summary: summary, adding: model.hasConfig, busy: busy, onCancel: { self.preview = nil }) {
                    Task { await save(yaml) }
                }
            case .kube(let yaml, let summary, let conflicts)?:
                KubePreviewList(summary: summary, conflicts: conflicts, busy: busy, onCancel: { self.preview = nil }) { choices in
                    Task { await saveKube(yaml, summary: summary, conflicts: conflicts, choices: choices) }
                }
            case nil:
                picker
            }
        }
        .navigationTitle("Add a cluster")
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Button { showingHelp = true } label: { Image(systemName: "questionmark.circle") }
                    .accessibilityLabel("How to create a talosconfig")
            }
        }
        .sheet(isPresented: $showingHelp) { HelpSheet() }
        .sheet(isPresented: $showingDiscovery) {
            CloudDiscoveryView { found in validate(found.kubeconfig, discovered: found) }
        }
        // A file opened with Ichor (IncomingConfig): previewed like a picked one.
        .task(id: NotificationRouter.shared.pendingImportText) {
            guard let text = NotificationRouter.shared.pendingImportText else { return }
            NotificationRouter.shared.pendingImportText = nil
            preview = nil
            validate(text)
        }
    }

    private var picker: some View {
        VStack(spacing: 16) {
            VStack(spacing: 8) {
                Text("No cluster yet? Explore Ichor with a sample Talos cluster.")
                    .font(.callout).foregroundStyle(.secondary)
                Button("Try demo") {
                    busy = true
                    Task {
                        do { await save(try await TalosClient.demoConfig(), replacingSameCluster: true) }
                        catch { self.error = error.localizedDescription; busy = false }
                    }
                }
                .buttonStyle(.bordered)
                .disabled(busy)
                // A restore replaces every cluster: offered when there is none yet.
                if !model.hasConfig {
                    Text("Moving from another phone? Restore the backup you made there.")
                        .font(.callout).foregroundStyle(.secondary)
                    RestoreBackupButton(confirmFirst: false, onRestored: onImported) { error = $0 }
                        .buttonStyle(.bordered)
                        .disabled(busy)
                }
            }
            Button { showingDiscovery = true } label: {
                Label("Add from a cloud account", systemImage: "cloud")
            }
            .buttonStyle(.bordered)
            .disabled(busy)
            Picker("Source", selection: $source) {
                ForEach(Source.allCases) { Text($0.label).tag($0) }
            }
            .pickerStyle(.segmented)

            if let error { Text(error).foregroundStyle(.statusBad).font(.footnote) }
            if busy { ProgressView() }

            switch source {
            case .file:
                Text("Select your talosconfig (~/.talos/config) or kubeconfig (~/.kube/config) from your workstation, e.g. shared via AirDrop to Files.")
                    .font(.callout).foregroundStyle(.secondary)
                // On the button, not the screen: the restore button has a file importer of its own.
                // Any file: a kubeconfig is often named "config", with no extension.
                Button("Choose file") { showingImporter = true }
                    .buttonStyle(.borderedProminent)
                    .fileImporter(isPresented: $showingImporter, allowedContentTypes: [.yaml, .plainText, .data, .item]) { result in
                        switch result {
                        case .success(let url): readFile(url)
                        case .failure(let failure): error = failure.localizedDescription
                        }
                    }
                Spacer()
            case .paste:
                TextEditor(text: $pasted)
                    .font(.system(.footnote, design: .monospaced))
                    .autocorrectionDisabled()
                    .textInputAutocapitalization(.never)
                    .border(.quaternary)
                Button("Validate") { validate(pasted) }
                    .buttonStyle(.borderedProminent)
                    .disabled(pasted.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            case .qr:
                QRScannerView { validate($0) }
            }
        }
        .padding()
    }

    private func readFile(_ url: URL) {
        let scoped = url.startAccessingSecurityScopedResource()
        defer { if scoped { url.stopAccessingSecurityScopedResource() } }
        do {
            let data = try Data(contentsOf: url)
            guard data.count <= IncomingConfig.maxBytes, let text = String(data: data, encoding: .utf8) else {
                error = String(localized: "This file is not a talosconfig or a kubeconfig.")
                return
            }
            validate(text)
        } catch {
            self.error = error.localizedDescription
        }
    }

    /// Expands a compressed payload (a large config in a QR code), then takes the talosconfig
    /// or the kubeconfig flow.
    private func validate(_ text: String, discovered found: DiscoveredClusters? = nil) {
        busy = true
        discovered = found
        Task {
            defer { busy = false }
            do {
                let yaml = try await TalosClient.decodeImportText(text)
                if await TalosClient.isKubeconfig(yaml) {
                    let summary = try await TalosClient.parseKubeconfig(yaml)
                    let conflicts = try await TalosClient.kubeImportConflicts(
                        stored: model.kubeYAML ?? "", talos: model.yaml ?? "", added: yaml)
                    preview = .kube(yaml: yaml, summary: summary, conflicts: conflicts)
                } else {
                    preview = .talos(yaml: yaml, summary: try await TalosClient.parse(yaml))
                }
                error = nil
            } catch {
                self.error = error.localizedDescription
            }
        }
    }

    private func save(_ yaml: String, replacingSameCluster: Bool = false) async {
        busy = true
        defer { busy = false }
        do {
            try await model.save(yaml: yaml, replacingSameCluster: replacingSameCluster)
            onImported()
        } catch {
            self.error = error.localizedDescription
            preview = nil
        }
    }

    private func saveKube(_ yaml: String, summary: ConfigSummary, conflicts: [KubeImportConflict],
                          choices: [KubeImportChoice]) async {
        busy = true
        defer { busy = false }
        do {
            try await model.saveKube(added: yaml, choices: choices)
        } catch {
            self.error = error.localizedDescription
            preview = nil
            return
        }
        guard let found = discovered else {
            onImported()
            return
        }
        discovered = nil
        let failures = await signInDiscovered(summary: summary, conflicts: conflicts, choices: choices, secrets: found.secrets)
        if failures.isEmpty {
            onImported()
        } else {
            // The clusters are added; those not signed in say so on their home.
            error = String(localized: "Clusters added. Some could not be signed in: \(failures.joined(separator: "; "))")
            preview = nil
        }
    }

    /// Signs the discovered clusters just added in with the account's credentials, those that
    /// sign in with credentials; the others (OIDC, Azure device code) wait for the user. The
    /// errors of the others, by context.
    private func signInDiscovered(summary: ConfigSummary, conflicts: [KubeImportConflict],
                                  choices: [KubeImportChoice], secrets: String) async -> [String] {
        guard let kube = model.kubeYAML else { return [] }
        let skipped = Set(choices.filter { $0.skip == true }.map(\.index))
        let names = importedSignInContexts(
            summary: summary,
            selected: Set(summary.contexts.indices).subtracting(skipped),
            replacing: Set(choices.filter { $0.replace == true }.map(\.index)),
            conflicts: conflicts.map { (index: $0.index, suggested: $0.suggested) }
        )
        var failures: [String] = []
        for name in names {
            do {
                try await TalosClient.setCredentials(kube: kube, context: name, secrets: secrets)
            } catch {
                let message = error.localizedDescription
                if !isNotCredentialsMethod(message) && !isKubeSignInRequired(message) {
                    failures.append("\(name): \(message)")
                }
            }
        }
        model.reloadKubernetes()
        return failures
    }
}

private struct PreviewList: View {
    let summary: ConfigSummary
    /// A config is already stored: this one is added to it.
    let adding: Bool
    let busy: Bool
    let onCancel: () -> Void
    let onImport: () -> Void

    var body: some View {
        List {
            Section {
                Text("Config is valid").font(.headline)
            }
            ForEach(summary.contexts) { ctx in
                Section(ctx.name == summary.current ? String(localized: "\(ctx.name) (current)") : ctx.name) {
                    LabeledContent("Endpoints", value: ctx.endpoints.joined(separator: "\n"))
                    let noNodes = ctx.omni ? String(localized: "learned from Omni") : String(localized: "endpoints")
                    LabeledContent("Nodes", value: ctx.nodes.isEmpty ? noNodes : "\(ctx.nodes.count)")
                    if ctx.omni {
                        LabeledContent("Omni cluster", value: ctx.cluster ?? "")
                        LabeledContent("Omni identity", value: ctx.identity ?? "")
                    } else {
                        LabeledContent("Roles", value: ctx.roles.joined(separator: ", "))
                        LabeledContent("Cert expires", value: localizedCertExpiry(ctx.certNotAfter))
                    }
                }
            }
            if adding {
                Section {
                    Text("It is added to the clusters already on this device. A cluster imported before (same name and CA) is updated.")
                        .font(.footnote)
                }
            }
            Section {
                Text("Stored in the Keychain on this device only, never synced or backed up.")
                    .font(.footnote).foregroundStyle(.secondary)
                Button("Import", action: onImport).disabled(busy)
                Button("Cancel", role: .cancel, action: onCancel)
            }
        }
    }
}

/// The contexts of an imported kubeconfig, one section each: on (importable ones) or off, how
/// it signs in, and why it cannot be added; a name already taken gets another one, or replaces
/// the stored context of the same cluster.
private struct KubePreviewList: View {
    let summary: ConfigSummary
    let conflicts: [KubeImportConflict]
    let busy: Bool
    let onCancel: () -> Void
    let onImport: ([KubeImportChoice]) -> Void

    @State private var selected: Set<Int>
    @State private var replacing: Set<Int> = []

    init(summary: ConfigSummary, conflicts: [KubeImportConflict], busy: Bool,
         onCancel: @escaping () -> Void, onImport: @escaping ([KubeImportChoice]) -> Void) {
        self.summary = summary
        self.conflicts = conflicts
        self.busy = busy
        self.onCancel = onCancel
        self.onImport = onImport
        _selected = State(initialValue: summary.importableIndices)
    }

    var body: some View {
        List {
            Section {
                Text("Choose the clusters to add").font(.headline)
            } footer: {
                Text("A client certificate or a token works as it is. A cluster that signs in (OIDC, EKS, GKE, AKS, DigitalOcean, Rancher) asks for it once added.")
            }
            ForEach(Array(summary.contexts.enumerated()), id: \.element.id) { index, ctx in
                Section {
                    row(ctx, index: index)
                }
            }
            Section {
                Text("Stored in the Keychain on this device only, never synced or backed up.")
                    .font(.footnote).foregroundStyle(.secondary)
                Button("Import") {
                    onImport(kubeImportChoices(count: summary.contexts.count, selected: selected, replacing: replacing))
                }
                .disabled(busy || selected.isEmpty)
                Button("Cancel", role: .cancel, action: onCancel)
            }
        }
    }

    @ViewBuilder
    private func row(_ ctx: ContextSummary, index: Int) -> some View {
        // EKS and GKE name contexts by ARN or gke_project_location_name: show the cluster's name.
        let cloud = parseCloudContext(ctx.name)
        let shown = cloud?.cluster ?? ctx.name
        Toggle(isOn: member(index, of: $selected)) {
            Text(verbatim: ctx.name == summary.current ? String(localized: "\(shown) (current)") : shown)
                .font(.headline)
        }
        .disabled(ctx.problem != nil)
        if let cloud { Text(verbatim: cloud.localizedDetail).font(.footnote).foregroundStyle(.secondary) }
        LabeledContent("Server", value: ctx.endpoints.first ?? "")
        LabeledContent("Sign-in", value: ctx.localizedAuthLabel)
        if ctx.problem == nil, let method = ctx.signIn {
            Text("Signs in with \(KubeAuthWording.methodLabel(method)) once added.")
                .font(.footnote).foregroundStyle(.secondary)
        }
        if let user = ctx.user, !user.isEmpty { LabeledContent("Signed in as", value: user) }
        if let namespace = ctx.namespace, !namespace.isEmpty { LabeledContent("Namespace", value: namespace) }
        if ctx.certNotAfter > 0 { LabeledContent("Expires", value: localizedCertExpiry(ctx.certNotAfter)) }
        if ctx.insecure {
            Text("The server certificate is not checked (insecure-skip-tls-verify).")
                .font(.footnote).foregroundStyle(.statusWarn)
        }
        if let problem = ctx.localizedKubeProblem {
            Text(problem).font(.footnote).foregroundStyle(.statusBad)
        } else if let conflict = conflicts.first(where: { $0.index == index }) {
            if let sameAs = conflict.sameAs {
                Toggle(String(localized: "Replace \(sameAs)"), isOn: member(index, of: $replacing))
                    .disabled(!selected.contains(index))
            }
            if !replacing.contains(index) {
                Text("The name is taken: added as \(conflict.suggested).")
                    .font(.footnote).foregroundStyle(.secondary)
            }
        }
    }

    /// Whether `index` is in `set`, as a toggle sets it.
    private func member(_ index: Int, of set: Binding<Set<Int>>) -> Binding<Bool> {
        Binding(
            get: { set.wrappedValue.contains(index) },
            set: { on in
                if on { set.wrappedValue.insert(index) } else { set.wrappedValue.remove(index) }
            }
        )
    }
}
