import SwiftUI
import IchorCore

/// Crossplane (os:admin): its packages, the APIs its XRDs define, and every composite, claim
/// and managed resource with its Ready and Synced state, problems first. A tap opens the
/// object's summary; nothing is changed here.
struct CrossplaneView: View {
    @Environment(AppModel.self) private var model
    @State private var state: LoadState<Crossplane> = .loading
    @State private var query = ""
    @State private var filter: CrossplaneFilter = .all
    @State private var pending: PendingCrossplaneAction?
    @State private var actionError: String?

    var body: some View {
        LoadStateView(state: state, retry: load) { xp in
            list(xp)
        }
        .navigationTitle(Text("Crossplane"))
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .refreshable { await load() }
    }

    private func load() async {
        guard let client = model.client else { return }
        let loaded: LoadState<Crossplane> = await .from { try await client.crossplane() }
        state = state.refreshed(with: loaded)
    }

    /// Asks whether the credentials may patch the object, then shows the confirmation.
    private func pick(_ object: CrossplaneObject, _ action: CrossplaneAction, composite: Bool) {
        Task {
            var denial: KubeAccess?
            if let client = model.client,
                let access = try? await client.kubeCan(
                    verb: "patch", group: object.ref.group, resource: object.ref.resource,
                    namespace: object.ref.namespace, name: object.ref.name),
                access.isDenied
            {
                denial = access
            }
            pending = PendingCrossplaneAction(object: object, action: action, composite: composite, denial: denial)
        }
    }

    private func run(_ p: PendingCrossplaneAction) async {
        guard let client = model.client else { return }
        do {
            try await client.crossplaneAction(ref: p.object.ref, action: p.action)
            await load()
        } catch {
            actionError = String(
                localized: "\(p.actionTitle) on \(p.object.label) failed: \(error.localizedDescription)")
        }
    }

    @ViewBuilder
    private func list(_ xp: Crossplane) -> some View {
        List {
            if !xp.error.isEmpty {
                Section {
                    Text(verbatim: String(localized: "Some sections could not be read: \(xp.error)")).font(.callout)
                        .foregroundStyle(.secondary)
                }
            }
            if !xp.installed || (xp.packages.isEmpty && xp.definitions.isEmpty && xp.rows.isEmpty) {
                Section { Text("Crossplane is installed but defines nothing yet.").foregroundStyle(.secondary) }
            } else {
                Section {
                    counts(xp)
                    Picker("Filter", selection: $filter) {
                        Text("All").tag(CrossplaneFilter.all)
                        Text("Critical").tag(CrossplaneFilter.critical)
                        Text("Warning").tag(CrossplaneFilter.warning)
                    }
                    .pickerStyle(.segmented)
                    TextField("Search kind, name or external name", text: $query)
                    if xp.truncated {
                        Text(
                            "Long list: healthy managed resources past the first 500 are left out; the search looks at the ones listed."
                        )
                        .font(.caption).foregroundStyle(.secondary)
                    }
                }
                let problems = xp.problems
                if !problems.isEmpty {
                    Section("Problems") {
                        ForEach(problems) { o in
                            ObjectRow(
                                object: o,
                                onPick: xp.claims.contains(o)
                                    ? nil : { pick(o, $0, composite: xp.composites.contains(o)) })
                        }
                    }
                }
                if !xp.packages.isEmpty {
                    Section("Providers & packages") {
                        ForEach(xp.packages) { PackageRow(package: $0) }
                    }
                }
                if !xp.definitions.isEmpty {
                    Section("Definitions") {
                        ForEach(xp.definitions) { DefinitionRow(definition: $0) }
                    }
                }
                let composites = xp.composites.filtered(filter, query)
                let claims = xp.claims.filtered(filter, query)
                let managed = xp.managed.filtered(filter, query)
                if !composites.isEmpty || !claims.isEmpty {
                    Section("Composite resources & claims") {
                        ForEach(composites) { o in ObjectRow(object: o) { pick(o, $0, composite: true) } }
                        // A claim is changed through its composite: no actions on it.
                        ForEach(claims) { ObjectRow(object: $0) }
                    }
                }
                if !managed.isEmpty {
                    Section("Managed resources") {
                        ForEach(managed) { o in ObjectRow(object: o) { pick(o, $0, composite: false) } }
                    }
                }
            }
        }
        .themedBackground()
        .confirmationDialog(
            pending?.title ?? "", isPresented: Binding(get: { pending != nil }, set: { if !$0 { pending = nil } }),
            titleVisibility: .visible, presenting: pending
        ) { p in
            if p.denial == nil {
                Button(p.actionTitle) { Task { await run(p) } }
            }
        } message: { p in
            Text(verbatim: p.message)
        }
        .alert(
            String(localized: "Crossplane"),
            isPresented: Binding(get: { actionError != nil }, set: { if !$0 { actionError = nil } })
        ) {
            Button("OK") { actionError = nil }
        } message: {
            Text(verbatim: actionError ?? "")
        }
    }

    @ViewBuilder
    private func counts(_ xp: Crossplane) -> some View {
        HStack(spacing: 12) {
            Text(verbatim: String(localized: "\(xp.counts.critical) critical"))
                .font(.callout.weight(.medium))
                .foregroundStyle(xp.counts.critical > 0 ? Color.statusBad : .secondary)
            Text(verbatim: String(localized: "\(xp.counts.warning) warning"))
                .font(.callout.weight(.medium))
                .foregroundStyle(xp.counts.warning > 0 ? Color.statusWarn : .secondary)
            Text(verbatim: String(localized: "\(xp.counts.ok) ok"))
                .font(.callout.weight(.medium)).foregroundStyle(.secondary)
            Spacer()
        }
        Text(verbatim: String(localized: "\(xp.managed.count) of \(xp.managedTotal) managed resources listed"))
            .font(.caption).foregroundStyle(.secondary)
    }
}

/// One row: health dot, kind and name, then the details that matter; pushes the object summary.
private struct RowShell: View {
    let health: ServiceHealth
    let title: String
    let line: String
    let resource: KubeAPIResource
    let namespace: String
    let name: String

    var body: some View {
        NavigationLink {
            KubeObjectView(resource: resource, namespace: namespace, name: name)
        } label: {
            HStack(alignment: .top, spacing: 12) {
                HealthDot(health: health).padding(.top, 5)
                VStack(alignment: .leading, spacing: 2) {
                    Text(verbatim: title).font(.subheadline.monospaced()).lineLimit(1)
                    if !line.isEmpty {
                        Text(verbatim: line).font(.caption).foregroundStyle(.secondary).lineLimit(3)
                    }
                }
            }
        }
    }
}

private func objectResource(_ ref: CrossplaneRef) -> KubeAPIResource {
    KubeAPIResource(
        group: ref.group, version: ref.version, resource: ref.resource, kind: ref.kind,
        namespaced: !ref.namespace.isEmpty)
}

private struct PackageRow: View {
    let package: CrossplanePackage

    var body: some View {
        RowShell(
            health: package.health,
            title: "\(package.ref.kind) \(package.label)",
            line: [
                package.package, package.currentRevision, conditionText(package.installed),
                conditionText(package.healthy),
            ]
            .compactMap { $0 }.joined(separator: " · ")
                + package.reasons.map(reasonText).map { " · \($0)" }.joined(),
            resource: objectResource(package.ref),
            namespace: package.ref.namespace,
            name: package.ref.name,
        )
    }
}

private struct DefinitionRow: View {
    let definition: CrossplaneDefinition

    var body: some View {
        RowShell(
            health: definition.health,
            title: definition.label,
            line: [
                definition.compositeKind.isEmpty ? nil : "\(definition.compositeGroup)/\(definition.compositeKind)",
                definition.scope.isEmpty ? nil : definition.scope,
                definition.claimKind.isEmpty ? nil : String(localized: "claim kind \(definition.claimKind)"),
                String(localized: "\(definition.compositions) compositions"),
                String(localized: "\(definition.composites) composites"),
                conditionText(definition.established),
            ]
            .compactMap { $0 }.joined(separator: " · ")
                + definition.reasons.map(reasonText).map { " · \($0)" }.joined(),
            resource: objectResource(definition.ref),
            namespace: definition.ref.namespace,
            name: definition.ref.name,
        )
    }
}

/// An action picked on an object, with whether the credentials may run it, to confirm.
private struct PendingCrossplaneAction {
    let object: CrossplaneObject
    let action: CrossplaneAction
    let composite: Bool
    let denial: KubeAccess?

    var actionTitle: String { crossplaneActionTitle(action) }

    var title: String {
        switch action {
        case .pause: String(localized: "Pause \(object.label)?")
        case .resume: String(localized: "Resume \(object.label)?")
        case .reconcile: String(localized: "Reconcile \(object.label) now?")
        }
    }

    /// What it does; a pause also says it blocks deletion and, on a composite, not its parts.
    var message: String {
        var parts: [String]
        switch action {
        case .pause:
            parts = [
                String(
                    localized:
                        "Crossplane stops reconciling it until it is resumed: no more calls to the provider’s API. A paused resource cannot be deleted."
                )
            ]
            if composite {
                parts.append(String(localized: "Its composed resources keep reconciling: each has its own pause."))
            }
        case .resume:
            parts = [String(localized: "Crossplane reconciles it again, calling the provider’s API as before.")]
        case .reconcile: parts = [String(localized: "Crossplane reconciles it now instead of at its next poll.")]
        }
        if let denial { parts.append(denial.deniedText) }
        return parts.joined(separator: "\n\n")
    }
}

private func crossplaneActionTitle(_ action: CrossplaneAction) -> String {
    switch action {
    case .pause: String(localized: "Pause")
    case .resume: String(localized: "Resume")
    case .reconcile: String(localized: "Reconcile now")
    }
}

private struct ObjectRow: View {
    let object: CrossplaneObject
    /// Pause, resume or reconcile: nil on a claim (changed through its composite).
    var onPick: ((CrossplaneAction) -> Void)?

    var body: some View {
        RowShell(
            health: object.health,
            title: "\(object.ref.kind) \(object.label)",
            line: [
                object.composition.isEmpty ? nil : String(localized: "composition \(object.composition)"),
                object.resources > 0 ? String(localized: "\(object.resources) resources") : nil,
                object.claim.isEmpty ? nil : String(localized: "claim \(object.claim)"),
                object.composite.isEmpty ? nil : String(localized: "composite \(object.composite)"),
                object.providerConfig.isEmpty ? nil : String(localized: "provider config \(object.providerConfig)"),
                object.externalName.isEmpty ? nil : String(localized: "external name \(object.externalName)"),
                conditionText(object.ready),
                conditionText(object.synced),
                object.synced.message.isEmpty ? nil : object.synced.message,
                object.paused ? String(localized: "paused") : nil,
            ]
            .compactMap { $0 }.joined(separator: " · ")
                + object.reasons.map(reasonText).map { " · \($0)" }.joined(),
            resource: objectResource(object.ref),
            namespace: object.ref.namespace,
            name: object.ref.name,
        )
        .contextMenu {
            if let onPick {
                ForEach(object.actions, id: \.self) { action in
                    Button(crossplaneActionTitle(action)) { onPick(action) }
                }
            }
        }
        .swipeActions(edge: .trailing) {
            if let onPick {
                ForEach(object.actions, id: \.self) { action in
                    Button(crossplaneActionTitle(action)) { onPick(action) }
                        .tint(action == .pause ? .orange : .blue)
                }
            }
        }
    }
}

/// "True Available · 3 hours ago", or nil when the condition says nothing.
private func conditionText(_ c: CrossplaneCondition) -> String? {
    if c.status.isEmpty { return nil }
    var parts = [c.status]
    if !c.reason.isEmpty { parts.append(c.reason) }
    if c.lastTransition > 0 { parts.append(relativeTime(c.lastTransition)) }
    return parts.joined(separator: " ")
}

private func reasonText(_ reason: CrossplaneReason) -> String {
    switch reason {
    case .syncFailed: String(localized: "Cannot sync")
    case .providerUnhealthy: String(localized: "Provider unhealthy")
    case .notReady: String(localized: "Not ready")
    case .deleting: String(localized: "Deleting")
    case .paused: String(localized: "Paused")
    case .notEstablished: String(localized: "Definition not established")
    }
}
