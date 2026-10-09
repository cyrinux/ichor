import SwiftUI
import IchorCore

// The sections of ArgoAppView, top to bottom (the timeline is in ArgoWaveTimeline.swift).

/// Icon, name, health and sync badges and the actions, then where the app comes from and goes
/// to, its links and the auto-sync switch (disabled, with the reason, for an owned app).
struct ArgoHero: View {
    let app: ArgoApp
    let busy: Bool
    let sync: () -> Void
    let act: (ArgoAction) -> Void
    let terminate: () -> Void

    var body: some View {
        Section {
            VStack(spacing: 12) {
                AppIconView(app: app.iconApp, size: 64)
                VStack(spacing: 2) {
                    Text(verbatim: app.name).font(.title2.bold()).multilineTextAlignment(.center).accessibilityAddTraits(.isHeader)
                    RevisionLink(label: app.revisionLabel, url: app.revisionURL)
                        .font(.subheadline.monospaced())
                        .foregroundStyle(.secondary)
                }
                HStack(spacing: 8) {
                    ArgoBadge(label: app.health.label, symbol: app.health.symbol, color: app.health.color)
                    ArgoBadge(label: app.sync.label, symbol: app.sync.symbol, color: app.sync.color)
                }
                actions
                KubeDeniedNote(.argoSync, in: app.namespace)
                    .multilineTextAlignment(.center)
            }
            .frame(maxWidth: .infinity)
            .padding(.vertical, 8)
        }
        .listRowBackground(Color.clear)
        .listRowInsets(EdgeInsets())
        Section {
            LabeledContent("Project", value: app.project)
            LabeledContent("Destination") {
                Text(verbatim: destination).multilineTextAlignment(.trailing)
            }
            ForEach(app.sources.indices, id: \.self) { i in
                let source = app.sources[i]
                VStack(alignment: .leading, spacing: 2) {
                    Text("Source").font(.caption).foregroundStyle(.secondary)
                    if let url = webURL(source.repoURL) {
                        Link(destination: url) {
                            Label { Text(verbatim: source.repo).lineLimit(2).truncationMode(.middle) } icon: { Image(systemName: "arrow.up.forward.square") }
                                .font(.callout.monospaced())
                        }
                    } else {
                        Text(verbatim: source.repo).font(.callout.monospaced()).lineLimit(2).truncationMode(.middle).textSelection(.enabled)
                    }
                    Text(verbatim: [source.chart.isEmpty ? source.path : source.chart, source.targetRevision].filter { !$0.isEmpty }.joined(separator: " @ "))
                        .font(.caption.monospaced())
                        .foregroundStyle(.secondary)
                }
            }
            if app.deployedAt > 0 { LabeledContent("Deployed", value: relativeTime(app.deployedAt)) }
            ForEach(app.externalURLs, id: \.self) { link in
                if let url = URL(string: link), url.scheme == "https" || url.scheme == "http" {
                    Link(destination: url) {
                        Label { Text(verbatim: url.host() ?? link).lineLimit(1) } icon: { Image(systemName: "safari") }
                    }
                }
            }
            Toggle(isOn: Binding(get: { app.autoSync.enabled }, set: { act($0 ? .autoSyncOn : .autoSyncOff) })) {
                Text("Auto-sync")
                if app.autoSync.enabled {
                    Text(verbatim: [app.autoSync.prune ? String(localized: "prune") : nil,
                                    app.autoSync.selfHeal ? String(localized: "self-heal") : nil].compactMap { $0 }.joined(separator: " · "))
                }
            }
            .disabled(!app.canChangeSpec || busy)
            .kubeGated(.argoSync, in: app.namespace)
        } footer: {
            if let notice = app.ownerNotice { Text(notice) }
        }
    }

    private var actions: some View {
        HStack(spacing: 10) {
            Button(action: sync) {
                Label("Sync", systemImage: "arrow.triangle.2.circlepath")
            }
            .buttonStyle(.borderedProminent)
            .disabled(!app.canSync || busy)
            Button { act(.refresh) } label: {
                Label("Refresh", systemImage: "arrow.clockwise")
            }
            .buttonStyle(.bordered)
            .disabled(busy)
            Menu {
                Button { act(.hardRefresh) } label: { Label("Hard refresh", systemImage: "arrow.clockwise.circle") }
                if app.canTerminate {
                    Button(role: .destructive, action: terminate) { Label("Terminate sync…", systemImage: "stop.circle") }
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
        // Every Argo CD action is a patch of the Application.
        .kubeGated(.argoSync, in: app.namespace)
        .overlay(alignment: .trailing) {
            if busy { ProgressView().offset(x: 30) }
        }
    }

    private var destination: String {
        let cluster = app.destination.name.isEmpty ? app.destination.server : app.destination.name
        let inCluster = cluster.isEmpty || cluster == "https://kubernetes.default.svc" || cluster == "in-cluster"
        return [app.destination.namespace, inCluster ? nil : cluster].compactMap { $0?.isEmpty == false ? $0 : nil }.joined(separator: " @ ")
    }
}

/// The likely cause when it points at a pod or a node, the health message, then Argo CD's conditions.
struct ArgoConditionsSection: View {
    let app: ArgoApp
    let downNodes: Set<String>

    var body: some View {
        let podCause = app.likelyCause(downNodes: downNodes).flatMap { $0.pointsAtPod ? $0 : nil }
        let message = app.health != .healthy && !app.healthMessage.isEmpty ? app.healthMessage : nil
        if podCause != nil || message != nil || !app.conditions.isEmpty {
            Section {
                if let podCause { banner(podCause.text, symbol: "exclamationmark.triangle.fill", color: .red) }
                if let message { banner(message, symbol: app.health.symbol, color: app.health.color) }
                ForEach(app.conditions.indices, id: \.self) { i in
                    let condition = app.conditions[i]
                    banner(condition.message, title: condition.type,
                           symbol: condition.isError ? "exclamationmark.octagon.fill" : "exclamationmark.triangle.fill",
                           color: condition.isError ? .red : attentionColor)
                }
            }
        }
    }

    private func banner(_ text: String, title: String? = nil, symbol: String, color: Color) -> some View {
        Label {
            VStack(alignment: .leading, spacing: 2) {
                if let title { Text(verbatim: title).font(.caption.weight(.semibold)) }
                Text(verbatim: text).font(.callout).textSelection(.enabled)
            }
        } icon: {
            Image(systemName: symbol).foregroundStyle(color)
        }
    }
}

/// The running or last sync: phase, who started it, how long, how far, what failed.
struct ArgoOperationSection: View {
    let operation: ArgoOperation
    let canTerminate: Bool
    let terminate: () -> Void

    var body: some View {
        Section(operation.phase.active ? String(localized: "Sync in progress") : String(localized: "Last sync")) {
            HStack {
                StatusPill(label: operation.phase.label, color: operation.phase.color)
                if operation.dryRun { InfoChip(text: String(localized: "dry run")) }
                Spacer()
                elapsed.font(.caption.monospacedDigit()).foregroundStyle(.secondary)
            }
            if operation.total > 0 {
                VStack(alignment: .leading, spacing: 4) {
                    ProgressView(value: Double(operation.done), total: Double(operation.total))
                        .tint(operation.phase.failed ? .red : operation.phase.active ? .blue : .green)
                    Text("\(operation.done) of \(operation.total) resources")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .monospacedDigit()
                }
            }
            if !operation.message.isEmpty {
                Text(verbatim: operation.message).font(.callout).foregroundStyle(.secondary).textSelection(.enabled)
            }
            LabeledContent("Started by", value: operation.initiatedBy == "automated" ? String(localized: "auto-sync") : operation.initiatedBy)
            if !operation.revision.isEmpty {
                LabeledContent("Revision") {
                    RevisionLink(label: shortRevision(operation.revision), url: operation.revisionURL).font(.callout.monospaced())
                }
            }
            if operation.retryCount > 0 { LabeledContent("Retries", value: "\(operation.retryCount)") }
            ForEach(operation.failed) { failed in
                VStack(alignment: .leading, spacing: 2) {
                    Label { Text(verbatim: "\(failed.kind) \(failed.name)").font(.callout.monospaced()) } icon: {
                        Image(systemName: "xmark.octagon.fill").foregroundStyle(.statusBad)
                    }
                    if !failed.message.isEmpty {
                        Text(verbatim: failed.message).font(.caption).foregroundStyle(.statusBad).textSelection(.enabled)
                    }
                }
            }
            if canTerminate {
                Button(role: .destructive, action: terminate) { Label("Terminate sync…", systemImage: "stop.circle") }
            }
        }
    }

    /// Ticking while it runs.
    @ViewBuilder private var elapsed: some View {
        if operation.startedAt > 0 {
            TimelineView(.periodic(from: .now, by: 1)) { context in
                let end = operation.finishedAt > 0 ? operation.finishedAt : context.date.epochMillis
                let seconds = max(0, (end - operation.startedAt) / 1000)
                Text(verbatim: Duration.seconds(seconds).formatted(.units(allowed: [.hours, .minutes, .seconds], width: .narrow)))
            }
        }
    }
}

/// Pods of the app's namespace that are not ready, those on a node that is down first.
struct ArgoPodsSection: View {
    let pods: [KubePod]
    let downNodes: Set<String>

    var body: some View {
        Section("Pods not ready") {
            ForEach(pods.sorted { downNodes.contains($0.node) && !downNodes.contains($1.node) }) { pod in
                VStack(alignment: .leading, spacing: 2) {
                    HStack(spacing: 8) {
                        Circle().fill(pod.transitional ? attentionColor : .red).frame(width: 8, height: 8)
                            .accessibilityHidden(true)
                        Text(verbatim: pod.name).font(.callout.monospaced()).lineLimit(1).truncationMode(.middle)
                    }
                    Text(verbatim: [pod.status, String(localized: "\(pod.ready)/\(pod.containers) ready"),
                                    pod.restarts > 0 ? String(localized: "\(pod.restarts) restarts") : nil]
                        .compactMap { $0 }.joined(separator: " · "))
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    if !pod.node.isEmpty {
                        Text(downNodes.contains(pod.node) ? String(localized: "\(pod.node) is not ready") : String(localized: "on \(pod.node)"))
                            .font(.caption)
                            .foregroundStyle(downNodes.contains(pod.node) ? Color.red : Color.secondary)
                    }
                }
                .accessibilityElement(children: .combine)
            }
        }
    }
}

/// The deployments, newest (current) first, with "Roll back to this" when the app allows it.
/// An http(s) URL the system can open, nil for anything else (or "").
func webURL(_ string: String) -> URL? {
    guard let url = URL(string: string), url.scheme == "https" || url.scheme == "http" else { return nil }
    return url
}

/// A revision label that opens its commit page when the Go core found one.
struct RevisionLink: View {
    let label: String
    let url: String

    var body: some View {
        if let url = webURL(url) {
            Link(destination: url) {
                HStack(spacing: 4) {
                    Text(verbatim: label)
                    Image(systemName: "arrow.up.forward.square").imageScale(.small)
                }
            }
            .accessibilityLabel(Text("Open commit"))
            .accessibilityValue(Text(verbatim: label))
        } else {
            Text(verbatim: label)
        }
    }
}

struct ArgoHistorySection: View {
    let app: ArgoApp
    let rollback: (ArgoHistory) -> Void

    var body: some View {
        Section {
            ForEach(Array(app.history.enumerated()), id: \.element.id) { index, entry in
                HStack(alignment: .top, spacing: 12) {
                    Image(systemName: index == 0 ? "smallcircle.filled.circle.fill" : "circle")
                        .foregroundStyle(index == 0 ? Color.green : Color.secondary)
                        .font(.footnote)
                        .padding(.top, 2)
                    VStack(alignment: .leading, spacing: 2) {
                        HStack(spacing: 6) {
                            RevisionLink(label: entry.label, url: entry.url).font(.callout.monospaced())
                            if index == 0 { InfoChip(text: String(localized: "current"), color: .green) }
                        }
                        Text(verbatim: [relativeTime(entry.deployedAt),
                                        entry.initiatedBy == "automated" ? String(localized: "auto-sync") : entry.initiatedBy]
                            .filter { !$0.isEmpty }.joined(separator: " · "))
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                    Spacer(minLength: 4)
                    if index > 0 && app.canRollback {
                        Button("Roll back to this") { rollback(entry) }
                            .buttonStyle(.bordered)
                            .controlSize(.small)
                            .kubeGated(.argoSync, in: app.namespace)
                    }
                }
            }
        } header: {
            Text("History")
        } footer: {
            if app.history.count > 1 && !app.canRollback {
                if let notice = app.ownerNotice {
                    Text(notice)
                } else if app.autoSync.enabled {
                    Text("Pause auto-sync to roll back: otherwise Argo CD syncs straight back to the latest revision.")
                }
            }
        }
    }
}
