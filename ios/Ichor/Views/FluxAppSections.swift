import SwiftUI
import IchorCore

// The sections of FluxAppView, top to bottom (the pods are Argo CD's ArgoPodsSection).

/// Icon, name, kind, state badge and the actions, then where it comes from (source and URL,
/// path or chart), the revision applied (and the one tried when they differ), interval, owner,
/// dependencies and failures.
struct FluxHero: View {
    let app: FluxApp
    /// The source listed, for what it follows and fetched; nil when not listed.
    let source: FluxSource?
    let busy: Bool
    let act: (FluxAction) -> Void

    var body: some View {
        Section {
            VStack(spacing: 12) {
                AppIconView(app: app.iconApp, size: 64)
                VStack(spacing: 2) {
                    Text(verbatim: app.name).font(.title2.bold()).multilineTextAlignment(.center).accessibilityAddTraits(.isHeader)
                    Text(verbatim: "\(app.kind) · \(app.namespace)").font(.subheadline).foregroundStyle(.secondary)
                    Text(verbatim: app.revisionLabel).font(.subheadline.monospaced()).foregroundStyle(.secondary)
                }
                HStack(spacing: 8) {
                    ArgoBadge(label: app.state.label, symbol: app.state.symbol, color: app.state.color)
                    if app.stalled { ArgoBadge(label: String(localized: "Stalled"), symbol: "exclamationmark.octagon", color: .red) }
                }
                actions
            }
            .frame(maxWidth: .infinity)
            .padding(.vertical, 8)
        }
        .listRowBackground(Color.clear)
        .listRowInsets(EdgeInsets())
        Section {
            readiness
            sourceRow
            if app.isHelmRelease {
                if !app.origin.isEmpty { LabeledContent("Chart") { Text(verbatim: app.origin).font(.callout.monospaced()) } }
            } else if !app.path.isEmpty {
                LabeledContent("Path") { Text(verbatim: app.path).font(.callout.monospaced()) }
            }
            if !app.revision.isEmpty {
                LabeledContent("Revision") { Text(verbatim: fluxShortRevision(app.revision)).font(.callout.monospaced()) }
            }
            if app.revisionDiffers {
                LabeledContent("Attempted") {
                    Text(verbatim: fluxShortRevision(app.attemptedRevision)).font(.callout.monospaced()).foregroundStyle(attentionColor)
                }
            }
            if !app.interval.isEmpty { LabeledContent("Interval", value: app.interval) }
            if !app.targetNamespace.isEmpty { LabeledContent("Target namespace", value: app.targetNamespace) }
            if app.reconciledAt > 0 { LabeledContent("Last change", value: relativeTime(app.reconciledAt)) }
            if let owner = app.owner {
                LabeledContent("Applied by") { Text(verbatim: owner.label).multilineTextAlignment(.trailing) }
            }
            if !app.dependsOn.isEmpty {
                LabeledContent("Depends on") {
                    Text(verbatim: app.dependsOn.joined(separator: "\n")).font(.callout.monospaced()).multilineTextAlignment(.trailing)
                }
            }
            if app.failures > 0 {
                Label(String(localized: "\(app.failures) failed attempts in a row"), systemImage: "exclamationmark.triangle.fill")
                    .foregroundStyle(.statusBad)
            }
        } footer: {
            if let notice = app.ownerNotice { Text(notice) }
        }
    }

    /// Ready (or not) with its reason, then the message.
    private var readiness: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text("Ready")
                Spacer()
                Text(verbatim: [app.ready, app.reason].filter { !$0.isEmpty }.joined(separator: " · "))
                    .foregroundStyle(app.state == .failing ? Color.red : Color.secondary)
            }
            if !app.message.isEmpty {
                Text(verbatim: app.message).font(.caption).foregroundStyle(.secondary).textSelection(.enabled)
            }
            if app.suspended {
                Label("Suspended: Flux does not reconcile it until it is resumed.", systemImage: "pause.circle.fill")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
    }

    @ViewBuilder private var sourceRow: some View {
        if let ref = app.source {
            VStack(alignment: .leading, spacing: 2) {
                Text("Source").font(.caption).foregroundStyle(.secondary)
                Label { Text(verbatim: ref.label).font(.callout) } icon: { Image(systemName: fluxKindSymbol(ref.kind)) }
                if !app.sourceURL.isEmpty {
                    Text(verbatim: app.sourceURL).font(.callout.monospaced()).lineLimit(2).truncationMode(.middle).textSelection(.enabled)
                }
                if let source {
                    Text(verbatim: [source.ref, source.revisionLabel].filter { !$0.isEmpty }.joined(separator: " · "))
                        .font(.caption.monospaced())
                        .foregroundStyle(.secondary)
                    if source.state == .failing, !source.message.isEmpty {
                        Text(verbatim: source.message).font(.caption).foregroundStyle(.statusBad).lineLimit(3)
                    }
                }
            }
        }
    }

    private var actions: some View {
        HStack(spacing: 10) {
            Button { act(.reconcile) } label: {
                Label("Reconcile", systemImage: FluxAction.reconcile.symbol)
            }
            .buttonStyle(.borderedProminent)
            .disabled(!app.canReconcile || busy)
            let toggle: FluxAction = app.suspended ? .resume : .suspend
            Button { act(toggle) } label: {
                Label(toggle.label, systemImage: toggle.symbol)
            }
            .buttonStyle(.bordered)
            .disabled(busy)
            Menu {
                Button { act(.reconcileWithSource) } label: {
                    Label("Reconcile with source…", systemImage: FluxAction.reconcileWithSource.symbol)
                }
                .disabled(!app.canReconcileWithSource)
                if app.isHelmRelease {
                    Button { act(.force) } label: { Label("Force upgrade…", systemImage: FluxAction.force.symbol) }
                        .disabled(!app.canForce)
                    Button { act(.reset) } label: { Label("Reset failures…", systemImage: FluxAction.reset.symbol) }
                        .disabled(!app.canReset)
                }
            } label: {
                Image(systemName: "ellipsis")
                    .frame(height: 20)
                    .accessibilityLabel(Text("More actions"))
            }
            .buttonStyle(.bordered)
            .disabled(busy)
        }
        .controlSize(.regular)
        .overlay(alignment: .trailing) {
            if busy { ProgressView().offset(x: 30) }
        }
    }
}

/// Flux's conditions (the Ready one only when the hero does not already say it), each with its
/// reason, message and when it changed.
struct FluxConditionsSection: View {
    let app: FluxApp

    var body: some View {
        let shown = app.conditions.filter { !($0.type == "Ready" && $0.message == app.message) }
        if !shown.isEmpty {
            Section("Conditions") {
                ForEach(shown.indices, id: \.self) { i in
                    let condition = shown[i]
                    let style = look(condition)
                    Label {
                        VStack(alignment: .leading, spacing: 2) {
                            HStack {
                                Text(verbatim: [condition.type, condition.reason].filter { !$0.isEmpty }.joined(separator: " · "))
                                    .font(.caption.weight(.semibold))
                                Spacer()
                                if condition.at > 0 {
                                    Text(verbatim: relativeTime(condition.at)).font(.caption).foregroundStyle(.secondary)
                                }
                            }
                            if !condition.message.isEmpty {
                                Text(verbatim: condition.message).font(.callout).textSelection(.enabled)
                            }
                        }
                    } icon: {
                        Image(systemName: style.symbol).foregroundStyle(style.color)
                    }
                }
            }
        }
    }

    /// Reconciling and Stalled are abnormal when True; the others (Ready, Healthy, Released…)
    /// when False.
    private func look(_ c: FluxCondition) -> (symbol: String, color: Color) {
        switch (c.type, c.status) {
        case ("Reconciling", "True"): ("arrow.triangle.2.circlepath", Color.blue)
        case ("Stalled", "True"): ("exclamationmark.octagon.fill", Color.red)
        case ("Reconciling", "False"), ("Stalled", "False"), (_, "True"): ("checkmark.circle.fill", Color.green)
        case (_, "False"): ("xmark.octagon.fill", Color.red)
        default: ("questionmark.circle", Color.secondary)
        }
    }
}

/// The Kustomizations and HelmReleases a Kustomization applies from Git.
struct FluxChildrenSection: View {
    let apps: [FluxApp]
    let downNodes: Set<String>

    var body: some View {
        Section("Applies") {
            ForEach(apps) { child in
                NavigationLink {
                    FluxAppView(kind: child.kind, namespace: child.namespace, name: child.name, downNodes: downNodes)
                } label: {
                    HStack(spacing: 10) {
                        AppIconView(app: child.iconApp, size: 28)
                        VStack(alignment: .leading, spacing: 1) {
                            Text(verbatim: child.name).font(.callout)
                            Text(verbatim: child.kind).font(.caption).foregroundStyle(.secondary)
                        }
                        Spacer()
                        FluxGlyph(state: child.state, font: .caption)
                    }
                }
            }
        }
    }
}

/// A Kustomization's inventory, one disclosure per kind.
struct FluxInventorySection: View {
    let app: FluxApp

    var body: some View {
        Section {
            ForEach(app.resourcesByKind) { group in
                DisclosureGroup {
                    ForEach(group.resources) { resource in
                        Text(verbatim: resource.namespace.isEmpty ? resource.name : "\(resource.namespace)/\(resource.name)")
                            .font(.callout.monospaced())
                            .lineLimit(1)
                            .truncationMode(.middle)
                            .textSelection(.enabled)
                    }
                } label: {
                    HStack {
                        Text(verbatim: group.kind).font(.callout.weight(.medium))
                        Spacer()
                        Text(verbatim: "\(group.resources.count)").font(.callout).foregroundStyle(.secondary).monospacedDigit()
                    }
                }
            }
        } header: {
            Text("Inventory")
        } footer: {
            Text("\(app.resources.count) objects applied")
        }
    }
}

/// A HelmRelease's Helm releases, newest first.
struct FluxHistorySection: View {
    let history: [FluxHistory]

    var body: some View {
        Section("History") {
            ForEach(history) { entry in
                HStack(alignment: .top, spacing: 12) {
                    Text(verbatim: "#\(entry.version)").font(.callout.monospacedDigit()).foregroundStyle(.secondary)
                    VStack(alignment: .leading, spacing: 2) {
                        HStack(spacing: 6) {
                            Text(verbatim: entry.chartVersion).font(.callout.monospaced())
                            if !entry.appVersion.isEmpty {
                                Text(verbatim: entry.appVersion).font(.caption.monospaced()).foregroundStyle(.secondary)
                            }
                        }
                        if entry.deployedAt > 0 {
                            Text(verbatim: relativeTime(entry.deployedAt)).font(.caption).foregroundStyle(.secondary)
                        }
                    }
                    Spacer(minLength: 4)
                    if !entry.status.isEmpty { StatusPill(label: entry.status, color: color(entry.status)) }
                }
            }
        }
    }

    private func color(_ status: String) -> Color {
        switch status {
        case "deployed": .green
        case "failed": .red
        case "pending-install", "pending-upgrade", "pending-rollback": .blue
        default: .secondary
        }
    }
}
