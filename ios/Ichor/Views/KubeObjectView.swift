import SwiftUI
import UniformTypeIdentifiers
import IchorCore

/// One object of any kind: first its summary (health, conditions, owners up the chain, events,
/// metadata), then its YAML (numbered, searchable, copy and share) without managedFields, and
/// Edit when the kind may be updated. A Secret's
/// values stay hidden until asked for, behind Face ID / the passcode when the app lock is on,
/// and it cannot be edited while they are hidden. A pod also opens its logs and a port-forward.
/// Delete confirms with what the deletion would touch, then goes back (`onDeleted` first).
struct KubeObjectView: View {
    let resource: KubeAPIResource
    /// "" for a cluster-scoped object.
    let namespace: String
    let name: String
    /// Called once the object is deleted, before the view goes back (a list reads itself again).
    let onDeleted: (() -> Void)?

    // Explicit: the private @State makes the memberwise init private.
    init(resource: KubeAPIResource, namespace: String, name: String, onDeleted: (() -> Void)? = nil) {
        self.resource = resource
        self.namespace = namespace
        self.name = name
        self.onDeleted = onDeleted
    }

    private enum Tab: Hashable { case summary, yaml }

    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @Environment(\.scenePhase) private var scenePhase
    @State private var tab = Tab.summary
    @State private var state: LoadState<String> = .loading
    @State private var summary: LoadState<KubeObjectSummary> = .loading
    @State private var reveal = false
    @State private var query = ""
    @State private var message: String?
    @State private var editing: KubeEditTarget?
    @State private var logsPod: KubePod?
    @State private var forwarding = false
    @State private var confirmingDelete = false
    @State private var deleted = false
    /// What refuses the delete, once asked; nil while asking, when allowed or unknown.
    @State private var deleteDenial: KubeAccess?

    var body: some View {
        VStack(spacing: 0) {
            header
            switch tab {
            case .summary:
                LoadStateView(state: summary, retry: loadSummary) { summary in
                    List { KubeObjectSummarySections(summary: summary) }
                        .themedBackground()
                        .refreshable { await loadSummary() }
                }
            case .yaml:
                LoadStateView(state: state, retry: load) { yaml in
                    ConfigYamlLines(yaml: yaml, query: query, refresh: load)
                }
            }
        }
        .searchable(text: $query, prompt: Text("Filter lines"))
        .autocorrectionDisabled()
        .textInputAutocapitalization(.never)
        .navigationTitle(Text(verbatim: name))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar { toolbar }
        .sheet(item: $editing) { target in
            KubeObjectEditView(target: target) {
                Task {
                    await load()
                    await loadSummary()
                }
            }
        }
        .sheet(item: $logsPod) { PodLogsSheet(pod: $0) }
        .sheet(isPresented: $confirmingDelete, onDismiss: leaveIfDeleted) {
            KubeObjectDeleteSheet(resource: resource, namespace: namespace, name: name, denial: deleteDenial) {
                deleted = true
            }
        }
        .navigationDestination(isPresented: $forwarding) {
            PortForwardView(namespace: namespace, pod: name)
        }
        .messageAlert($message)
        .task(id: "\(kubeNamespacesKey(model))|\(reveal)") { await load() }
        .task(id: kubeNamespacesKey(model)) { await loadSummary() }
        .task(id: "\(kubeNamespacesKey(model))|\(scenePhase == .active)") { await followSummary() }
        .task(id: kubeNamespacesKey(model)) { await loadDeleteAccess() }
    }

    @ViewBuilder private var header: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 6) {
                Text(verbatim: resource.kind).font(.subheadline.weight(.semibold))
                if !namespace.isEmpty {
                    Text(verbatim: "· \(namespace)").font(.subheadline).foregroundStyle(.secondary)
                }
            }
            .lineLimit(1)
            Picker(selection: $tab) {
                Text(verbatim: SummaryText.tabSummary).tag(Tab.summary)
                Text(verbatim: "YAML").tag(Tab.yaml)
            } label: {
                EmptyView()
            }
            .pickerStyle(.segmented)
            if resource.isSecret && reveal {
                Label("Secret values are shown in clear. Don't share screenshots.", systemImage: "exclamationmark.triangle.fill")
                    .font(.footnote)
                    .foregroundStyle(.statusWarn)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal)
        .padding(.vertical, 8)
    }

    @ToolbarContentBuilder private var toolbar: some ToolbarContent {
        ToolbarItemGroup(placement: .primaryAction) {
            if resource.canUpdate, case .loaded = state {
                Button { startEditing() } label: { Label("Edit", systemImage: "pencil") }
            }
            Menu {
                if case .loaded(let yaml, _, _) = state {
                    Button { copy(yaml) } label: { Label("Copy", systemImage: "doc.on.doc") }
                    ShareLink(item: yaml) { Label("Share", systemImage: "square.and.arrow.up") }
                }
                if resource.isSecret {
                    Button { Task { await setReveal(!reveal) } } label: {
                        if reveal {
                            Label("Hide secret values", systemImage: "eye.slash")
                        } else {
                            Label("Show secret values", systemImage: "eye")
                        }
                    }
                }
                if resource.isPod {
                    Button { logsPod = KubePod(namespace: namespace, name: name) } label: {
                        Label("Logs", systemImage: "doc.text")
                    }
                    Button { forwarding = true } label: {
                        Label("Port forward", systemImage: "arrow.left.arrow.right.circle")
                    }
                }
                Divider()
                if let deleteDenial {
                    Button(role: .destructive) {} label: {
                        Text("Delete")
                        Text(verbatim: deleteDenial.deniedText)
                    }
                    .disabled(true)
                } else {
                    Button(role: .destructive) { confirmingDelete = true } label: {
                        Label("Delete", systemImage: "trash")
                    }
                }
            } label: {
                Image(systemName: "ellipsis.circle").accessibilityLabel(Text("More actions"))
            }
        }
    }

    /// The YAML, Secret values only when revealed; a newer toggle wins over an older load.
    private func load() async {
        guard let client = model.client else { return }
        let wanted = reveal
        let result: LoadState<String> = await .from {
            try await client.objectYAML(resource, namespace: namespace, name: name, reveal: wanted)
        }
        guard wanted == reveal else { return }
        state = state.refreshed(with: result)
    }

    private func loadSummary() async {
        guard let client = model.client else { return }
        let result: LoadState<KubeObjectSummary> = await .from {
            try await client.objectSummary(resource, namespace: namespace, name: name)
        }
        summary = summary.refreshed(with: result)
    }

    /// Keeps the summary live while the app is active: the Go core reads it again whenever the
    /// object or its events change. A watch that ends is followed again after a while; one
    /// refused leaves the one-shot read on screen.
    private func followSummary() async {
        guard scenePhase == .active else { return }
        while !Task.isCancelled {
            guard let client = model.client else { return }
            for await event in client.objectSummaryWatch(resource, namespace: namespace, name: name) {
                if case .update(let latest) = event { summary = .loaded(latest, at: Date()) }
            }
            try? await Task.sleep(for: .seconds(kubeWatchRetrySeconds))
        }
    }

    /// Asks whether the credentials may delete this object; an answer that cannot be had
    /// leaves Delete offered (the API server still decides).
    private func loadDeleteAccess() async {
        guard let client = model.client else { return }
        guard let access = try? await client.kubeCan(verb: "delete", group: resource.group, resource: resource.resource,
                                                     namespace: namespace, name: name),
              !Task.isCancelled else { return }
        deleteDenial = access.isDenied ? access : nil
    }

    /// Once the delete sheet closed after a deletion: tells the list, then goes back to it.
    private func leaveIfDeleted() {
        guard deleted else { return }
        onDeleted?()
        dismiss()
    }

    /// Showing a Secret's values needs a fresh Face ID / passcode when the app lock is on;
    /// hiding them reads the object again so they leave the screen and memory.
    private func setReveal(_ on: Bool) async {
        if on, model.lock.enabled,
           let failure = await Authenticator.authenticate(reason: String(localized: "Show the values of Secret \(name)")) {
            message = failure
            return
        }
        state = .loading
        reveal = on
    }

    /// Editing works on the YAML on screen; a Secret's hidden values would be saved as their
    /// placeholders, so they must be shown first.
    private func startEditing() {
        guard case .loaded(let yaml, _, _) = state else { return }
        if resource.isSecret && (!reveal || hasHiddenSecretValues(yaml)) {
            message = String(localized: "Show the Secret's values before editing it.")
            return
        }
        editing = KubeEditTarget(resource: resource, namespace: namespace, name: name, yaml: yaml)
    }

    /// Device-only pasteboard; revealed values expire from it after two minutes.
    private func copy(_ yaml: String) {
        let options: [UIPasteboard.OptionsKey: Any] = reveal
            ? [.localOnly: true, .expirationDate: Date().addingTimeInterval(120)]
            : [.localOnly: true]
        UIPasteboard.general.setItems([[UTType.utf8PlainText.identifier: yaml]], options: options)
        message = reveal ? String(localized: "Copied. The clipboard is cleared in 2 minutes.") : String(localized: "Copied.")
    }
}

/// The object an edit starts from: its YAML as read.
struct KubeEditTarget: Identifiable {
    let resource: KubeAPIResource
    let namespace: String
    let name: String
    let yaml: String

    var id: String { "\(resource.id)/\(namespace)/\(name)" }
}
