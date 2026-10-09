import SwiftUI
import IchorCore

/// The Kustomizations and HelmReleases of the chosen chip, each opening its screen: swipe right
/// to reconcile, left to suspend or resume (also in the context menu, with reconcile with source).
struct FluxAppsList: View {
    let status: FluxStatus
    @Binding var filter: FluxFilter
    let query: String
    let downNodes: Set<String>
    /// Objects an action is being sent to.
    let busy: Set<FluxRef>
    let refresh: () async -> Void
    let act: (FluxAction, FluxRef) -> Void

    var body: some View {
        let shown = filterFluxApps(status.apps, filter: filter, query: query)
        List {
            KubeDeniedSection(actions: [.fluxReconcile], namespace: kubeSharedNamespace(status.apps.filter(\.isKustomization).map(\.namespace)))
            if !status.helmError.isEmpty { Section { ErrorLine(error: status.helmError) } }
            ForEach(shown) { app in
                NavigationLink(value: FluxAppRoute(kind: app.kind, namespace: app.namespace, name: app.name, downNodes: downNodes)) {
                    FluxAppRow(app: app, busy: busy.contains(app.target))
                }
                .swipeActions(edge: .leading) {
                    Button { act(.reconcile, app.target) } label: { Label("Reconcile", systemImage: FluxAction.reconcile.symbol) }
                        .tint(.blue)
                        .disabled(!app.canReconcile)
                        .kubeGated(app.target.accessAction, in: app.namespace)
                }
                .swipeActions(edge: .trailing) {
                    suspendButton(suspended: app.suspended, target: app.target)
                        .kubeGated(app.target.accessAction, in: app.namespace)
                }
                .contextMenu {
                    Group {
                        Button { act(.reconcile, app.target) } label: { Label("Reconcile", systemImage: FluxAction.reconcile.symbol) }
                            .disabled(!app.canReconcile)
                        Button { act(.reconcileWithSource, app.target) } label: {
                            Label("Reconcile with source", systemImage: FluxAction.reconcileWithSource.symbol)
                        }
                        .disabled(!app.canReconcileWithSource)
                        suspendButton(suspended: app.suspended, target: app.target)
                    }
                    .kubeGated(app.target.accessAction, in: app.namespace)
                }
            }
        }
        .overlay {
            if shown.isEmpty {
                if !query.isEmpty {
                    ContentUnavailableView.search(text: query)
                } else if filter != .all {
                    ContentUnavailableView {
                        Label("Nothing here", systemImage: "checkmark.circle")
                    } description: {
                        Text("No app matches “\(filter.label)”.")
                    } actions: {
                        Button("Show all") { filter = .all }
                    }
                } else {
                    ContentUnavailableView("No Flux apps", systemImage: "arrow.triangle.branch")
                }
            }
        }
        .refreshable { await refresh() }
        .themedBackground()
    }

    private func suspendButton(suspended: Bool, target: FluxRef) -> some View {
        fluxSuspendButton(suspended: suspended) { act($0, target) }
    }
}

/// Suspend (or Resume for a suspended object), for swipes and menus.
func fluxSuspendButton(suspended: Bool, act: @escaping (FluxAction) -> Void) -> some View {
    let action: FluxAction = suspended ? .resume : .suspend
    return Button { act(action) } label: { Label(action.label, systemImage: action.symbol) }
        .tint(suspended ? Color.green : Color.gray)
}

/// Icon, name and kind, "main@4be1d0c · 2 hours ago", the reason when it is not ready, and the
/// ready glyph.
struct FluxAppRow: View {
    let app: FluxApp
    var busy = false

    var body: some View {
        HStack(spacing: 12) {
            AppIconView(app: app.iconApp, size: 40)
            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: 6) {
                    Text(verbatim: app.name).font(.body.weight(.medium)).lineLimit(1)
                    if let owner = app.owner { FluxOwnerBadge(owner: owner) }
                }
                Text(verbatim: detail)
                    .font(.caption.monospacedDigit())
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                    .truncationMode(.middle)
                if app.state == .failing || app.state == .reconciling, !app.summary.isEmpty {
                    Text(verbatim: app.state == .failing && !app.reason.isEmpty ? "\(app.reason): \(app.message)" : app.summary)
                        .font(.caption)
                        .foregroundStyle(app.state == .failing ? Color.red : Color.blue)
                        .lineLimit(2)
                }
            }
            Spacer(minLength: 4)
            if busy {
                ProgressView()
            } else {
                FluxGlyph(state: app.state)
            }
        }
        .padding(.vertical, 2)
    }

    private var detail: String {
        var parts = [app.kind, app.revisionLabel]
        if app.reconciledAt > 0 { parts.append(relativeTime(app.reconciledAt)) }
        if app.suspended { parts.append(String(localized: "suspended")) }
        if app.pending { parts.append(String(localized: "reconcile requested…")) }
        return parts.filter { !$0.isEmpty }.joined(separator: " · ")
    }
}

/// The Git, OCI and Helm repositories and buckets: URL, what they follow, the revision fetched
/// and when, how many apps use them; tap or swipe to reconcile, suspend or resume.
struct FluxSourcesList: View {
    let status: FluxStatus
    let query: String
    let busy: Set<FluxRef>
    let refresh: () async -> Void
    let act: (FluxAction, FluxRef) -> Void

    var body: some View {
        let shown = filterFluxSources(status.sources, query: query)
        List {
            if !status.sourcesError.isEmpty { Section { ErrorLine(error: status.sourcesError) } }
            ForEach(shown) { source in
                // A tap opens the actions too: sources have no screen, and the row looked
                // tappable while only a long press did anything.
                Menu {
                    Button { act(.reconcile, source.target) } label: { Label("Reconcile", systemImage: FluxAction.reconcile.symbol) }
                        .disabled(!source.canReconcile)
                    fluxSuspendButton(suspended: source.suspended) { act($0, source.target) }
                } label: {
                    FluxSourceRow(source: source, busy: busy.contains(source.target))
                        .foregroundStyle(.primary)
                        .contentShape(Rectangle())
                }
                .swipeActions(edge: .leading) {
                    Button { act(.reconcile, source.target) } label: { Label("Reconcile", systemImage: FluxAction.reconcile.symbol) }
                        .tint(.blue)
                        .disabled(!source.canReconcile)
                }
                .swipeActions(edge: .trailing) {
                    fluxSuspendButton(suspended: source.suspended) { act($0, source.target) }
                }
            }
        }
        .emptyOverlay(shown.isEmpty, query: query) { ContentUnavailableView("No Flux sources", systemImage: "arrow.triangle.branch") }
        .refreshable { await refresh() }
        .themedBackground()
    }
}

/// Kind and name, the URL, "main · main@4be1d0c · fetched 4 minutes ago · 3 apps", the reason
/// when it is not ready, and the ready glyph.
struct FluxSourceRow: View {
    let source: FluxSource
    var busy = false

    var body: some View {
        HStack(spacing: 12) {
            Image(systemName: fluxKindSymbol(source.kind))
                .font(.title3)
                .foregroundStyle(.secondary)
                .frame(width: 40, height: 40)
                .background(Color(.tertiarySystemFill), in: RoundedRectangle(cornerRadius: 10))
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: 6) {
                    Text(verbatim: source.name).font(.body.weight(.medium)).lineLimit(1)
                    Text(verbatim: source.kind).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                }
                if !source.url.isEmpty {
                    Text(verbatim: source.url)
                        .font(.caption.monospaced())
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                        .truncationMode(.middle)
                        .textSelection(.enabled)
                }
                Text(verbatim: detail)
                    .font(.caption.monospacedDigit())
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
                if source.state == .failing, !(source.message.isEmpty && source.reason.isEmpty) {
                    Text(verbatim: [source.reason, source.message].filter { !$0.isEmpty }.joined(separator: ": "))
                        .font(.caption)
                        .foregroundStyle(.red)
                        .lineLimit(3)
                }
            }
            Spacer(minLength: 4)
            if busy {
                ProgressView()
            } else {
                FluxGlyph(state: source.state)
            }
        }
        .padding(.vertical, 2)
    }

    private var detail: String {
        [source.ref,
         source.revisionLabel,
         source.fetchedAt > 0 ? String(localized: "fetched \(relativeTime(source.fetchedAt))") : nil,
         source.suspended ? String(localized: "suspended") : nil,
         String(localized: "\(source.apps) apps")]
            .compactMap { $0 }
            .filter { !$0.isEmpty }
            .joined(separator: " · ")
    }
}
