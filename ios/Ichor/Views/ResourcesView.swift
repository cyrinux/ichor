import SwiftUI
import IchorCore
import UniformTypeIdentifiers

/// Resource browser, like `talosctl get rd` then `talosctl get TYPE` (os:reader): the types
/// a node serves, grouped by namespace. Sensitive types carry a lock.
struct ResourceTypesView: View {
    let node: String
    let hostname: String

    // Explicit: the private @State properties make the memberwise init private.
    init(node: String, hostname: String) {
        self.node = node
        self.hostname = hostname
    }

    @Environment(AppModel.self) private var model
    @State private var state: LoadState<[ResourceType]> = .loading
    @State private var query = ""

    var body: some View {
        LoadStateView(state: state, retry: load) { types in
            let groups = groupResourceTypes(types, query: query)
            List {
                ForEach(groups) { group in
                    Section {
                        ForEach(group.types) { type in
                            NavigationLink {
                                ResourceItemsView(node: node, type: type)
                            } label: {
                                ResourceTypeRow(type: type)
                            }
                        }
                    } header: {
                        Text(verbatim: group.namespace.or("—")).textCase(nil)
                    }
                }
            }
            .emptyOverlay(groups.isEmpty, query: query) { ContentUnavailableView("No resource type", systemImage: "square.stack.3d.up") }
            .refreshable { await load() }
            .themedBackground()
        }
        .searchable(text: $query, prompt: Text("Search types and aliases"))
        .navigationTitle(String(localized: "Resources · \(hostname)"))
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
    }

    private func load() async {
        guard let client = model.client else { return }
        state = await .from { try await client.resourceTypes(node: node) }
    }
}

private struct ResourceTypeRow: View {
    let type: ResourceType

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack(spacing: 6) {
                Text(verbatim: type.shortName).font(.body)
                if type.isSensitive {
                    Image(systemName: "lock.fill")
                        .font(.caption)
                        .foregroundStyle(.statusWarn)
                        .accessibilityLabel(Text("Sensitive"))
                }
            }
            Text(verbatim: type.type).font(.caption.monospaced()).foregroundStyle(.secondary)
                .lineLimit(1).truncationMode(.middle)
            if !type.aliases.isEmpty {
                Text(verbatim: type.aliases.joined(separator: ", ")).font(.caption2).foregroundStyle(.secondary).lineLimit(1)
            }
        }
    }
}

/// The resources of one type on a node.
struct ResourceItemsView: View {
    let node: String
    let type: ResourceType

    // Explicit: the private @State properties make the memberwise init private.
    init(node: String, type: ResourceType) {
        self.node = node
        self.type = type
    }

    @Environment(AppModel.self) private var model
    @State private var state: LoadState<ResourceItems> = .loading
    @State private var query = ""

    var body: some View {
        LoadStateView(state: state, retry: load) { list in
            let shown = filterResourceItems(list.items, query: query)
            List {
                Section {
                    ForEach(shown) { item in
                        NavigationLink {
                            ResourceDetailView(node: node, type: type, item: item)
                        } label: {
                            ResourceItemRow(item: item)
                        }
                    }
                } footer: {
                    if list.truncated { Text("Only the first \(list.items.count) are shown") }
                }
            }
            .emptyOverlay(shown.isEmpty, query: query) { ContentUnavailableView("No resource of this type", systemImage: "square.stack.3d.up") }
            .refreshable { await load() }
            .themedBackground()
        }
        .searchable(text: $query, prompt: Text("Filter by ID"))
        .navigationTitle(type.shortName)
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
    }

    private func load() async {
        guard let client = model.client else { return }
        state = await .from { try await client.resourceList(node: node, namespace: type.namespace, type: type.type) }
    }
}

private struct ResourceItemRow: View {
    let item: ResourceItem

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(verbatim: item.id).font(.callout.monospaced()).lineLimit(2).truncationMode(.middle)
            HStack(spacing: 10) {
                if !item.version.isEmpty { Text("version \(item.version)") }
                if !item.phase.isEmpty { Text(verbatim: item.phase) }
                if item.updated > 0 {
                    Text(Date(epochMillis: item.updated), format: .relative(presentation: .named))
                }
            }
            .font(.caption)
            .foregroundStyle(.secondary)
            .monospacedDigit()
        }
    }
}

/// One resource as YAML (`talosctl get TYPE ID -o yaml`): monospaced, selectable, scrolling
/// sideways rather than wrapping. A sensitive resource is only fetched once the user asks
/// for it ("Show"), and is blurred again when the app leaves the foreground or the screen is
/// recorded (like the talosconfig QR code).
struct ResourceDetailView: View {
    let node: String
    let type: ResourceType
    let item: ResourceItem

    // Explicit: the private @State properties make the memberwise init private.
    init(node: String, type: ResourceType, item: ResourceItem) {
        self.node = node
        self.type = type
        self.item = item
    }

    @Environment(AppModel.self) private var model
    @Environment(\.scenePhase) private var scenePhase
    @State private var state: LoadState<ResourceDocument> = .loading
    /// A sensitive resource is not asked for before the first "Show".
    @State private var requested = false
    @State private var revealed = false
    @State private var captured = UIScreen.main.isCaptured
    @State private var message: String?

    private var visible: Bool { !type.isSensitive || (revealed && !captured && scenePhase == .active) }

    var body: some View {
        Group {
            if type.isSensitive && !requested {
                ContentUnavailableView {
                    Label("Sensitive", systemImage: "lock.fill")
                } description: {
                    Text("This resource contains secrets (keys, tokens). Show it only where nobody else can see your screen.")
                    if let message { Text(message) }
                } actions: {
                    Button("Show") { Task { await reveal() } }.buttonStyle(.borderedProminent)
                }
            } else {
                loadedContent
            }
        }
        .navigationTitle(item.id)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Button(action: copy) {
                    Label("Copy YAML", systemImage: "doc.on.doc")
                }
                .disabled(loadedYAML == nil || !visible)
            }
        }
        .onChange(of: scenePhase) { _, phase in
            if phase != .active { revealed = false }
        }
        .onReceive(NotificationCenter.default.publisher(for: UIScreen.capturedDidChangeNotification)) { _ in
            captured = UIScreen.main.isCaptured
            if captured { revealed = false }
        }
        .task { if !type.isSensitive { await load() } }
    }

    private var loadedContent: some View {
        LoadStateView(state: state, retry: load) { document in
            VStack(alignment: .leading, spacing: 0) {
                if type.isSensitive || message != nil { banner }
                ScrollView([.vertical, .horizontal]) {
                    Text(verbatim: document.yaml)
                        .font(.caption.monospaced())
                        .textSelection(.enabled)
                        .fixedSize()
                        .padding()
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .blur(radius: visible ? 0 : 12)
                        .allowsHitTesting(visible)
                        .accessibilityHidden(!visible)
                }
                .overlay {
                    if !visible {
                        VStack(spacing: 12) {
                            Image(systemName: "eye.slash").font(.largeTitle).foregroundStyle(.secondary)
                            if captured {
                                Text("Hidden while the screen is recorded or mirrored.").font(.footnote).foregroundStyle(.secondary)
                            } else {
                                Button("Show") { Task { await reveal() } }.buttonStyle(.borderedProminent)
                            }
                        }
                        .padding()
                    }
                }
            }
            .themedBackground()
        }
    }

    private var banner: some View {
        VStack(alignment: .leading, spacing: 4) {
            if type.isSensitive {
                Label("This resource contains secrets (keys, tokens). Show it only where nobody else can see your screen.", systemImage: "lock.fill")
                    .font(.footnote)
                    .foregroundStyle(.statusWarn)
            }
            if let message { Text(message).font(.footnote).foregroundStyle(.secondary) }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal)
        .padding(.vertical, 8)
    }

    private var loadedYAML: String? {
        if case .loaded(let document, _, _) = state { return document.yaml }
        return nil
    }

    private func load() async {
        guard let client = model.client else { return }
        state = await .from {
            try await client.resource(node: node, namespace: item.namespace.isEmpty ? type.namespace : item.namespace,
                                      type: type.type, id: item.id)
        }
    }

    /// With the app lock on, showing a sensitive resource needs a fresh Face ID / passcode.
    private func reveal() async {
        if model.lock.enabled, let failure = await Authenticator.authenticate(reason: String(localized: "Show a sensitive resource")) {
            message = failure
            return
        }
        message = nil
        revealed = true
        if !requested {
            requested = true
            await load()
        }
    }

    /// Device-only pasteboard; a sensitive resource expires from it after two minutes.
    private func copy() {
        guard let yaml = loadedYAML else { return }
        let options: [UIPasteboard.OptionsKey: Any] = type.isSensitive
            ? [.localOnly: true, .expirationDate: Date().addingTimeInterval(120)]
            : [.localOnly: true]
        UIPasteboard.general.setItems([[UTType.utf8PlainText.identifier: yaml]], options: options)
        message = type.isSensitive ? String(localized: "Copied. The clipboard is cleared in 2 minutes.") : String(localized: "Copied.")
    }
}
