import SwiftUI
import IchorCore

/// The app's resources by sync wave as a vertical stepper: done waves turn green, the wave a
/// sync works on pulses, a wave with a failed resource is red, the waves to come are hollow.
/// Each step lists its resources with their sync and health; while selecting, a tap ticks a
/// resource for a selective sync.
struct ArgoWaveTimeline: View {
    let steps: [ArgoWaveStep]
    /// A sync runs: the current wave pulses.
    let running: Bool
    let selecting: Bool
    @Binding var selection: Set<String>

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            ForEach(steps) { step in
                ArgoWaveStepView(step: step, last: step.id == steps.last?.id, running: running,
                                 selecting: selecting, selection: $selection)
            }
        }
        .padding(.vertical, 6)
    }
}

private struct ArgoWaveStepView: View {
    let step: ArgoWaveStep
    let last: Bool
    let running: Bool
    let selecting: Bool
    @Binding var selection: Set<String>

    @State private var expanded = false

    /// Long waves show a few resources until opened, except the one that needs a look.
    private static let collapsedCount = 5

    var body: some View {
        let open = expanded || step.state == .current || step.state == .failed || step.resources.count <= Self.collapsedCount + 1
        let shown = open ? step.resources : Array(step.resources.prefix(Self.collapsedCount))
        HStack(alignment: .top, spacing: 12) {
            marker
            VStack(alignment: .leading, spacing: 6) {
                header
                ForEach(shown) { resource in
                    ArgoResourceLine(resource: resource, selecting: selecting, selected: selection.contains(resource.id)) {
                        if selection.contains(resource.id) { selection.remove(resource.id) } else { selection.insert(resource.id) }
                    }
                }
                if !open {
                    Button {
                        withAnimation(.snappy) { expanded = true }
                    } label: {
                        Text("Show \(step.resources.count - shown.count) more").font(.caption.weight(.medium))
                    }
                    .buttonStyle(.borderless)
                }
            }
            .padding(.bottom, last ? 0 : 18)
        }
        .background(alignment: .topLeading) {
            if !last {
                Rectangle()
                    .fill(step.state == .done ? Color.green.opacity(0.6) : Color(.separator))
                    .frame(width: 2)
                    .padding(.leading, 12)
                    .padding(.top, 28)
                    .padding(.bottom, 2)
            }
        }
        .accessibilityElement(children: .contain)
    }

    private var marker: some View {
        Image(systemName: symbol)
            .font(.title3.weight(.semibold))
            .foregroundStyle(color)
            .symbolEffect(.pulse, isActive: step.state == .current && running)
            .frame(width: 26, height: 26)
            .background(Circle().fill(Color(.secondarySystemGroupedBackground)))
            .accessibilityLabel(Text(stateLabel))
    }

    private var header: some View {
        HStack(spacing: 8) {
            Text("Wave \(step.wave)").font(.subheadline.weight(.semibold)).monospacedDigit().accessibilityAddTraits(.isHeader)
            if step.hasHooks {
                Text("hooks")
                    .font(.caption2.weight(.medium))
                    .padding(.horizontal, 6)
                    .padding(.vertical, 1)
                    .background(Color.purple.opacity(0.15), in: Capsule())
                    .foregroundStyle(.purple)
            }
            if step.state == .current && running {
                Text(stateLabel).font(.caption.weight(.medium)).foregroundStyle(.blue)
            }
            Spacer()
            Text(verbatim: "\(step.done)/\(step.total)")
                .font(.caption.monospacedDigit())
                .foregroundStyle(.secondary)
        }
        .padding(.top, 3)
    }

    private var symbol: String {
        switch step.state {
        case .done: "checkmark.circle.fill"
        case .current: "arrow.triangle.2.circlepath.circle.fill"
        case .failed: "xmark.circle.fill"
        case .pending: "circle"
        }
    }

    private var color: Color {
        switch step.state {
        case .done: .green
        case .current: .blue
        case .failed: .red
        case .pending: .secondary
        }
    }

    private var stateLabel: String {
        switch step.state {
        case .done: String(localized: "done")
        case .current: running ? String(localized: "in progress") : String(localized: "pending")
        case .failed: String(localized: "failed")
        case .pending: String(localized: "pending")
        }
    }
}

/// Kind glyph, kind and name, then its sync and health; a tick while selecting.
private struct ArgoResourceLine: View {
    let resource: ArgoResource
    let selecting: Bool
    let selected: Bool
    let toggle: () -> Void

    var body: some View {
        if selecting {
            Button(action: toggle) { content }
                .buttonStyle(.plain)
                .accessibilityAddTraits(selected ? .isSelected : [])
        } else {
            content
        }
    }

    private var content: some View {
        HStack(spacing: 8) {
            if selecting {
                Image(systemName: selected ? "checkmark.circle.fill" : "circle")
                    .foregroundStyle(selected ? Color.accentColor : Color.secondary)
                    .accessibilityHidden(true)
            }
            Image(systemName: argoKindSymbol(resource.kind))
                .font(.caption)
                .foregroundStyle(.secondary)
                .frame(width: 16)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 0) {
                Text(verbatim: resource.name)
                    .font(.caption.monospaced())
                    .foregroundStyle(.primary)
                    .lineLimit(1)
                    .truncationMode(.middle)
                Text(verbatim: [resource.kind, resource.namespace].filter { !$0.isEmpty }.joined(separator: " · "))
                    .font(.caption2)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            }
            Spacer(minLength: 4)
            if resource.hook { tag(String(localized: "hook"), .purple) }
            if resource.prune { tag(String(localized: "prune"), .red) }
            if resource.syncFailed { tag(String(localized: "failed"), .red) }
            Circle().fill(resource.sync.color).frame(width: 7, height: 7)
                .accessibilityLabel(Text(resource.sync.label))
            if let health = resource.health {
                Image(systemName: health.symbol).font(.caption2).foregroundStyle(health.color)
                    .accessibilityLabel(Text(health.label))
            }
        }
        .contentShape(Rectangle())
    }

    private func tag(_ text: String, _ color: Color) -> some View {
        Text(verbatim: text)
            .font(.caption2.weight(.medium))
            .padding(.horizontal, 5)
            .padding(.vertical, 1)
            .foregroundStyle(color)
            .background(color.opacity(0.12), in: Capsule())
    }
}

/// An SF Symbol for a Kubernetes kind.
func argoKindSymbol(_ kind: String) -> String {
    switch kind {
    case "Deployment", "ReplicaSet": "shippingbox"
    case "StatefulSet": "cylinder"
    case "DaemonSet": "square.grid.3x3"
    case "Pod": "cube"
    case "Service", "Endpoints", "EndpointSlice": "network"
    case "Ingress", "HTTPRoute", "Gateway": "globe"
    case "ConfigMap": "doc.text"
    case "Secret", "SealedSecret", "ExternalSecret": "key"
    case "CustomResourceDefinition": "puzzlepiece.extension"
    case "Job": "hammer"
    case "CronJob": "clock"
    case "ServiceAccount": "person.badge.key"
    case "Namespace": "folder"
    case "PersistentVolumeClaim", "PersistentVolume", "StorageClass": "externaldrive"
    case "Role", "RoleBinding", "ClusterRole", "ClusterRoleBinding": "lock.shield"
    case "Application", "ApplicationSet": "arrow.triangle.branch"
    default: "cube.transparent"
    }
}
