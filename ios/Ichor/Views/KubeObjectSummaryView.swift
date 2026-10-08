import SwiftUI
import IchorCore

/// The summary of an object, as list sections: its health and the spec fields read first,
/// its conditions, who owns or manages it (each opens its own summary, or its Helm release),
/// its events and its metadata.
struct KubeObjectSummarySections: View {
    let summary: KubeObjectSummary

    var body: some View {
        let now = Int64(Date().timeIntervalSince1970 * 1000)
        Section { header }
        Section {
            if summary.conditions.isEmpty {
                Text(verbatim: SummaryText.noConditions).font(.caption).foregroundStyle(.secondary)
            }
            ForEach(Array(summary.conditions.enumerated()), id: \.offset) { _, condition in
                ConditionRow(condition: condition, now: now)
            }
        } header: {
            Text(verbatim: SummaryText.conditions)
        }
        if !summary.owners.isEmpty {
            Section {
                ForEach(Array(summary.owners.enumerated()), id: \.offset) { _, owner in
                    OwnerLink(owner: owner)
                }
            } header: {
                Text(verbatim: SummaryText.owners)
            }
        }
        Section {
            if summary.eventsError.isEmpty {
                KubeEventsList(events: summary.events, showObject: false)
            } else {
                Text(verbatim: SummaryText.eventsError(summary.eventsError)).font(.caption).foregroundStyle(.secondary)
            }
        } header: {
            Text(verbatim: CheckupText.kubeEventsTitle)
        }
        Section {
            metadata(now: now)
        } header: {
            Text(verbatim: SummaryText.metadata)
        }
    }

    private var healthText: String {
        switch summary.healthTone {
        case .good: SummaryText.healthOK
        case .warn: SummaryText.healthWarn
        case .bad: SummaryText.healthBad
        case .neutral: SummaryText.healthNone
        }
    }

    @ViewBuilder private var header: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 8) {
                InfoChip(text: healthText, color: summary.healthTone.color ?? .secondary)
                if !summary.healthReason.isEmpty {
                    Text(verbatim: summary.healthReason).font(.caption).foregroundStyle(.secondary).lineLimit(2)
                }
            }
            if !summary.phase.isEmpty {
                LabeledValue(label: SummaryText.phase, value: summary.phase)
            }
            ForEach(Array(summary.highlights.enumerated()), id: \.offset) { _, highlight in
                LabeledValue(label: highlightLabel(highlight.key), value: highlight.value)
            }
        }
        .padding(.vertical, 2)
    }

    private func highlightLabel(_ key: String) -> String {
        switch key {
        case "replicas": SummaryText.replicas
        case "selector": SummaryText.selector
        case "image": SummaryText.image
        case "node": SummaryText.node
        case "suspended": SummaryText.suspended
        case "schedule": SummaryText.schedule
        default: key
        }
    }

    @ViewBuilder private func metadata(now: Int64) -> some View {
        if summary.created > 0 {
            Text(verbatim: SummaryText.created(checkupAge(summary.created, now: now))).font(.caption).foregroundStyle(.secondary)
        }
        if summary.deleting > 0 {
            Text(verbatim: SummaryText.deleting(checkupAge(summary.deleting, now: now))).font(.caption).foregroundStyle(.statusWarn)
        }
        if !summary.finalizers.isEmpty {
            KeyValueList(title: SummaryText.finalizers, lines: summary.finalizers)
        }
        if !summary.labels.isEmpty {
            KeyValueList(title: SummaryText.labels, lines: summary.labels.sorted { $0.key < $1.key }.map { "\($0.key)=\($0.value)" })
        }
        if !summary.annotations.isEmpty {
            KeyValueList(title: SummaryText.annotations, lines: summary.annotations.sorted { $0.key < $1.key }.map { "\($0.key)=\($0.value)" })
        }
    }
}

/// A label and its monospaced value on one line.
private struct LabeledValue: View {
    let label: String
    let value: String

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 6) {
            Text(verbatim: label).font(.caption).foregroundStyle(.secondary)
            Text(verbatim: value).font(.caption.monospaced()).lineLimit(2).textSelection(.enabled)
        }
    }
}

/// A condition: its type tinted by tone, status and reason, when it changed, its message.
private struct ConditionRow: View {
    let condition: SummaryCondition
    let now: Int64

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: 6) {
                InfoChip(text: condition.type, color: condition.toneValue.color ?? .secondary)
                Text(verbatim: [condition.status, condition.reason].filter { !$0.isEmpty }.joined(separator: " · "))
                    .font(.caption).lineLimit(1)
                Spacer()
                if condition.lastTransition > 0 {
                    Text(verbatim: CheckupText.kubeEventsAgo(checkupAge(condition.lastTransition, now: now)))
                        .font(.caption).foregroundStyle(.secondary)
                }
            }
            if !condition.message.isEmpty {
                Text(verbatim: condition.message).font(.caption).lineLimit(6)
            }
        }
        .padding(.vertical, 2)
    }
}

/// An owner or manager: a link to its summary (or its Helm release) when it can open.
private struct OwnerLink: View {
    let owner: ObjectOwner

    var body: some View {
        if owner.isHelmRelease {
            NavigationLink { HelmReleaseView(namespace: owner.namespace, name: owner.name) } label: { label }
        } else if let resource = owner.apiResource {
            NavigationLink { KubeObjectView(resource: resource, namespace: owner.namespace, name: owner.name) } label: { label }
        } else {
            label
        }
    }

    private var tag: String? {
        switch owner.via {
        case ObjectOwner.viaFlux: "Flux"
        case ObjectOwner.viaArgo: "Argo CD"
        case ObjectOwner.viaHelm: SummaryText.helmRelease
        default: owner.controller ? SummaryText.controller : nil
        }
    }

    private var label: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: 6) {
                Text(verbatim: owner.isHelmRelease ? "Helm" : owner.kind).font(.subheadline.weight(.semibold))
                if let tag { InfoChip(text: tag) }
            }
            Text(verbatim: [owner.namespace, owner.name].filter { !$0.isEmpty }.joined(separator: "/"))
                .font(.caption.monospaced()).foregroundStyle(.secondary).lineLimit(1).truncationMode(.middle)
        }
    }
}

/// "key=value" lines under a title, the first few then "Show all".
private struct KeyValueList: View {
    let title: String
    let lines: [String]

    // Explicit: the private @State makes the memberwise init private.
    init(title: String, lines: [String]) {
        self.title = title
        self.lines = lines
    }

    @State private var all = false

    private let preview = 6

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(verbatim: title).font(.caption.weight(.semibold))
            ForEach(Array((all ? lines : Array(lines.prefix(preview))).enumerated()), id: \.offset) { _, line in
                Text(verbatim: line).font(.caption2.monospaced()).textSelection(.enabled)
            }
            if lines.count > preview {
                Button {
                    all.toggle()
                } label: {
                    Text(verbatim: all ? CheckupText.checkupShowLess : CheckupText.checkupShowAll("\(lines.count)")).font(.callout)
                }
                .buttonStyle(.borderless)
            }
        }
    }
}
