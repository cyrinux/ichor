import SwiftUI
import IchorCore

/// A screen the node hub pushes from one of its rows (one destination for every row).
enum NodeHubScreen: Hashable {
    case kernelLog, events, network, storage, images, resourceBrowser, hardware, kubePods
    case debugShell, netTools, capture
    case machineConfig, upgrade, maintenance, drain, nodeMeta

    @ViewBuilder
    func view(_ ref: NodeRef) -> some View {
        switch self {
        case .kernelLog: LogsView(node: ref.address, hostname: ref.hostname, service: nil)
        case .events: EventsView(node: ref.address, hostnames: [ref.address: ref.hostname])
        case .network: NetworkView(node: ref.address, hostname: ref.hostname)
        case .storage: StorageView(node: ref.address, hostname: ref.hostname)
        case .images: ImagesView(node: ref.address, hostname: ref.hostname)
        case .resourceBrowser: ResourceTypesView(node: ref.address, hostname: ref.hostname)
        case .hardware: HardwareView(node: ref.address, hostname: ref.hostname)
        case .kubePods: NodeKubePodsView(node: ref.address, hostname: ref.hostname)
        case .debugShell: DebugShellView(node: ref.address, hostname: ref.hostname)
        case .netTools: NetToolsView(node: ref.address, hostname: ref.hostname)
        case .capture: CaptureView(node: ref.address, hostname: ref.hostname)
        case .machineConfig: MachineConfigView(node: ref.address, hostname: ref.hostname)
        case .upgrade: UpgradeView(node: ref.address, hostname: ref.hostname)
        case .maintenance: MaintenanceView(node: ref.address, hostname: ref.hostname)
        case .drain: MaintenanceView(node: ref.address, hostname: ref.hostname, drainOnly: true)
        case .nodeMeta: NodeMetaEditorView(node: ref.address, hostname: ref.hostname)
        }
    }
}

/// A hub row: its title with the "Needs Talos vX" notice under it, disabled when this node's
/// Talos version lacks the feature.
struct NodeHubRowLabel: View {
    let title: String
    let systemImage: String
    var support = FeatureSupport(supported: true)

    var body: some View {
        Label {
            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                if let notice = support.localizedNotice {
                    Text(notice).font(.caption).foregroundStyle(.secondary)
                }
            }
        } icon: {
            Image(systemName: systemImage)
        }
    }
}

/// A hub row that runs `action` (opens a screen, a sheet or a confirmation).
struct NodeHubButton: View {
    let title: String
    let systemImage: String
    var support = FeatureSupport(supported: true)
    var role: ButtonRole?
    let action: () -> Void

    var body: some View {
        Button(role: role, action: action) {
            NodeHubRowLabel(title: title, systemImage: systemImage, support: support)
        }
        .disabled(!support.supported)
    }
}

/// The former tabs, one row each: opens the tabs screen on that tab.
struct NodeStatusSection: View {
    let ref: NodeRef
    @Binding var open: NodeHubScreen?

    @Environment(AppModel.self) private var model

    var body: some View {
        Section("Status") {
            tabRow(.services, systemImage: "gearshape.2")
            tabRow(.resources, systemImage: "gauge.with.dots.needle.33percent")
            tabRow(.live, systemImage: "chart.xyaxis.line")
            tabRow(.processes, systemImage: "list.number", support: support(.processes))
            // "Pods" next to "Kubernetes pods" would read as the same list: these are Talos's containers.
            tabRow(.pods, title: model.allows(.workloads) ? String(localized: "Containers") : nil,
                   systemImage: "shippingbox.circle", support: support(.containers))
            // Cgroups (a copy of /sys/fs/cgroup) only exists for admin configs.
            if model.allows(.cgroups) { tabRow(.cgroups, systemImage: "square.3.layers.3d") }
            // The Pods tab reads the containers through Talos (CRI); this asks the API server (os:admin).
            if model.allows(.workloads) {
                NodeHubButton(title: String(localized: "Kubernetes pods"), systemImage: "cube") { open = .kubePods }
            }
        }
    }

    private func tabRow(_ tab: NodeTabsView.Tab, title: String? = nil, systemImage: String,
                        support: FeatureSupport = FeatureSupport(supported: true)) -> some View {
        NavigationLink(value: Route.nodeTab(ref, tab: tab)) {
            NodeHubRowLabel(title: title ?? tab.label, systemImage: systemImage, support: support)
        }
        .disabled(!support.supported)
    }

    private func support(_ feature: NodeFeature) -> FeatureSupport {
        model.support(feature, node: ref.address)
    }
}

/// Read-only screens about the node. Entries the role does not allow are hidden; entries this
/// node's Talos version lacks stay, disabled, saying which version they need.
struct NodeInspectSection: View {
    let node: String
    @Binding var open: NodeHubScreen?

    @Environment(AppModel.self) private var model

    var body: some View {
        Section("Inspect") {
            NodeHubButton(title: String(localized: "Kernel log"), systemImage: "terminal") { open = .kernelLog }
                .accessibilityIdentifier(SmokeID.kernelLog)
            NodeHubButton(title: String(localized: "Events"), systemImage: "list.bullet.rectangle", support: support(.events)) {
                open = .events
            }
            NodeHubButton(title: String(localized: "Network"), systemImage: "network", support: support(.network)) {
                open = .network
            }
            NodeHubButton(title: String(localized: "Storage"), systemImage: "internaldrive", support: support(.mounts)) {
                open = .storage
            }
            NodeHubButton(title: String(localized: "Images"), systemImage: "shippingbox", support: support(.images)) {
                open = .images
            }
            if model.allows(.resourceBrowser) {
                NodeHubButton(title: String(localized: "Resources browser"), systemImage: "square.stack.3d.up",
                              support: support(.resourceBrowser)) { open = .resourceBrowser }
            }
            NodeHubButton(title: String(localized: "About this node"), systemImage: "info.circle", support: support(.hardware)) {
                open = .hardware
            }
        }
    }

    private func support(_ feature: NodeFeature) -> FeatureSupport {
        model.support(feature, node: node)
    }
}

/// The netshoot debug shell, its one-check tools and packet captures; nothing without the role.
struct NodeTroubleshootSection: View {
    let node: String
    @Binding var open: NodeHubScreen?

    @Environment(AppModel.self) private var model

    var body: some View {
        // A section the role leaves empty would still show its title.
        if model.allows(.debugShell) || model.allows(.packetCapture) {
            Section("Troubleshoot") {
                if model.allows(.debugShell) {
                    NodeHubButton(title: String(localized: "Debug shell"), systemImage: "apple.terminal", support: support(.debugShell)) {
                        open = .debugShell
                    }
                    // The same privileged netshoot container, one check at a time.
                    NodeHubButton(title: String(localized: "Network tools"), systemImage: "network", support: support(.debugShell)) {
                        open = .netTools
                    }
                }
                if model.allows(.packetCapture) {
                    NodeHubButton(title: String(localized: "Capture packets"), systemImage: "antenna.radiowaves.left.and.right",
                                  support: support(.packetCapture)) { open = .capture }
                }
            }
        }
    }

    private func support(_ feature: NodeFeature) -> FeatureSupport {
        model.support(feature, node: node)
    }
}

/// Machine config, upgrade, reset, maintenance, drain and cordon; nothing without the role.
struct NodeOperateSection: View {
    let node: String
    /// A maintenance of this node runs: it owns the node's cordon (and reset) until it ends.
    let maintenanceRunning: Bool
    @Binding var open: NodeHubScreen?
    @Binding var showingReset: Bool
    @Binding var showingCordon: Bool

    @Environment(AppModel.self) private var model

    var body: some View {
        if model.allows(.machineConfig) || model.allows(.upgrade) || model.allows(.workloads) || model.allows(.reset) {
            Section("Operate") {
                if model.allows(.machineConfig) {
                    NodeHubButton(title: String(localized: "Machine config"), systemImage: "doc.text", support: support(.machineConfig)) {
                        open = .machineConfig
                    }
                }
                if model.allows(.upgrade) {
                    NodeHubButton(title: String(localized: "Upgrade Talos…"), systemImage: "arrow.up.circle", support: support(.upgrade)) {
                        open = .upgrade
                    }
                }
                if model.allows(.reset) {
                    NodeHubButton(title: String(localized: "Reset…"), systemImage: "arrow.counterclockwise.circle", support: support(.reset)) {
                        showingReset = true
                    }
                    .disabled(maintenanceRunning)
                }
                // Cordon and drain go through the Kubernetes API (os:admin, like the workloads).
                if model.allows(.workloads) {
                    NodeHubButton(title: String(localized: "Maintenance…"), systemImage: "wrench.and.screwdriver") { open = .maintenance }
                    NodeHubButton(title: String(localized: "Drain…"), systemImage: "rectangle.portrait.and.arrow.right") { open = .drain }
                    // An uncordon mid-drain lets the evicted pods come back before the reboot.
                    NodeHubButton(title: String(localized: "Cordon / uncordon…"), systemImage: "nosign") { showingCordon = true }
                        .disabled(maintenanceRunning)
                    NodeHubButton(title: String(localized: "Taints and labels…"), systemImage: "tag") { open = .nodeMeta }
                }
            }
        }
    }

    private func support(_ feature: NodeFeature) -> FeatureSupport {
        model.support(feature, node: node)
    }
}

/// Reboot and shut down, each behind its confirmation sheet; only for roles that allow them.
struct NodePowerSection: View {
    @Binding var powerAction: PowerAction?

    @Environment(AppModel.self) private var model

    var body: some View {
        if model.allows(.power) {
            Section("Power") {
                ForEach(PowerAction.allCases) { action in
                    NodeHubButton(title: action.localizedTitle, systemImage: "power", role: .destructive) { powerAction = action }
                }
            }
        }
    }
}
