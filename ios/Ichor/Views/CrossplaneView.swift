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

    @ViewBuilder
    private func list(_ xp: Crossplane) -> some View {
        List {
            if !xp.error.isEmpty {
                Section { Text(verbatim: String(localized: "Some sections could not be read: \(xp.error)")).font(.callout).foregroundStyle(.secondary) }
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
                        Text("Long list: healthy managed resources past the first 500 are left out; the search looks at the ones listed.")
                            .font(.caption).foregroundStyle(.secondary)
                    }
                }
                let problems = xp.problems
                if !problems.isEmpty {
                    Section("Problems") {
                        ForEach(problems) { ObjectRow(object: $0) }
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
                        ForEach(composites) { ObjectRow(object: $0) }
                        ForEach(claims) { ObjectRow(object: $0) }
                    }
                }
                if !managed.isEmpty {
                    Section("Managed resources") {
                        ForEach(managed) { ObjectRow(object: $0) }
                    }
                }
            }
        }
        .themedBackground()
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
    KubeAPIResource(group: ref.group, version: ref.version, resource: ref.resource, kind: ref.kind, namespaced: !ref.namespace.isEmpty)
}

private struct PackageRow: View {
    let package: CrossplanePackage

    var body: some View {
        RowShell(
            health: package.health,
            title: "\(package.ref.kind) \(package.label)",
            line: [package.package, package.currentRevision, conditionText(package.installed), conditionText(package.healthy)]
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
            line: [definition.compositeKind.isEmpty ? nil : "\(definition.compositeGroup)/\(definition.compositeKind)",
                   definition.scope.isEmpty ? nil : definition.scope,
                   definition.claimKind.isEmpty ? nil : String(localized: "claim kind \(definition.claimKind)"),
                   String(localized: "\(definition.compositions) compositions"),
                   String(localized: "\(definition.composites) composites"),
                   conditionText(definition.established)]
                .compactMap { $0 }.joined(separator: " · ")
                + definition.reasons.map(reasonText).map { " · \($0)" }.joined(),
            resource: objectResource(definition.ref),
            namespace: definition.ref.namespace,
            name: definition.ref.name,
        )
    }
}

private struct ObjectRow: View {
    let object: CrossplaneObject

    var body: some View {
        RowShell(
            health: object.health,
            title: "\(object.ref.kind) \(object.label)",
            line: [object.composition.isEmpty ? nil : String(localized: "composition \(object.composition)"),
                   object.resources > 0 ? String(localized: "\(object.resources) resources") : nil,
                   object.claim.isEmpty ? nil : String(localized: "claim \(object.claim)"),
                   object.composite.isEmpty ? nil : String(localized: "composite \(object.composite)"),
                   object.providerConfig.isEmpty ? nil : String(localized: "provider config \(object.providerConfig)"),
                   object.externalName.isEmpty ? nil : String(localized: "external name \(object.externalName)"),
                   conditionText(object.ready),
                   conditionText(object.synced),
                   object.synced.message.isEmpty ? nil : object.synced.message,
                   object.paused ? String(localized: "paused") : nil]
                .compactMap { $0 }.joined(separator: " · ")
                + object.reasons.map(reasonText).map { " · \($0)" }.joined(),
            resource: objectResource(object.ref),
            namespace: object.ref.namespace,
            name: object.ref.name,
        )
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
