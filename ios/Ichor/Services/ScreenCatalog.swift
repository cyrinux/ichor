import Foundation
import IchorCore

/// A screen the search can open (U3 step 5): its localised title, the section shown under it,
/// its SF Symbol and the route it pushes. Same entries and ids as Android's ScreenCatalog.kt.
struct ScreenSpec: Identifiable {
    let id: String
    let title: String
    let section: String
    let systemImage: String
    var subtitle = ""
    /// Words the title does not say ("hardware" for About this node).
    var keywords: [String] = []
    let route: Route

    var entry: ScreenEntry { ScreenEntry(id: id, title: title, subtitle: subtitle, keywords: keywords, section: section) }
}

/// Every screen of the active cluster the user may open, gated like the menus that lead to
/// them: no Talos screen for a cluster added from a kubeconfig, the Kubernetes ones only with a
/// role that reads the API, the upgrades only with one that may upgrade. `nodes` (the last
/// overview) adds each node's screens; none right after a cold start.
@MainActor
func screenCatalog(model: AppModel, nodes: [NodeOverview]) -> [ScreenSpec] {
    guard let context = model.activeSummary else { return [] }
    let talos = !context.isKube
    let kube = model.allows(.workloads)
    let upgrade = model.allows(.upgrade)
    let cluster = String(localized: "Overview")
    let operate = String(localized: "Operate")
    let hostnames = Dictionary(nodes.map { ($0.node, $0.hostname) }, uniquingKeysWith: { first, _ in first })
    var specs: [ScreenSpec] = []
    func add(_ id: String, _ title: String, _ section: String, _ image: String, _ route: Route, when shown: Bool = true) {
        if shown { specs.append(ScreenSpec(id: id, title: title, section: section, systemImage: image, route: route)) }
    }
    if talos { add("nodes", String(localized: "Nodes"), cluster, "server.rack", .nodes(filter: nil, nodes: nodes)) }
    add("health", String(localized: "Cluster health"), cluster, "heart", .health, when: model.allows(.health))
    add("etcd", "etcd", cluster, "cylinder", .etcd, when: talos)
    add("events", String(localized: "Events"), cluster, "timeline.selection", .events(node: nil, hostnames: hostnames), when: talos)
    add("kubespan", "KubeSpan", cluster, "point.3.connected.trianglepath.dotted", .kubespan, when: talos)
    add("workloads", String(localized: "Workloads"), cluster, "square.stack.3d.up", .workloads, when: kube)
    add("alerts", String(localized: "Alerts"), cluster, "bell", .alerts, when: kube)
    add("metrics", String(localized: "Metrics"), cluster, "chart.xyaxis.line", .metrics, when: kube)
    add("argocd", "Argo CD", cluster, "arrow.triangle.2.circlepath", .argoCD(downNodes: []), when: kube)
    add("flux", "Flux", cluster, "arrow.triangle.branch", .flux(downNodes: []), when: kube)
    add("data-services", String(localized: "Data services"), cluster, "cylinder.split.1x2", .dataServices(hints: "", downNodes: []), when: kube)
    add("checkup", String(localized: "Cluster checkup"), cluster, "checklist.checked", .checkup, when: kube)
    add("activity", String(localized: "Activity"), cluster, "list.bullet.rectangle", .activity(cluster: model.activeContext))
    // As the cluster title menu offers them (on Omni, Add a node opens the boot media).
    add("upgrade-cluster", String(localized: "Upgrade cluster"), operate, "arrow.up.circle", .clusterUpgrade(version: ""), when: upgrade)
    add("upgrade-kubernetes", String(localized: "Upgrade Kubernetes"), operate, "shippingbox", .k8sUpgrade, when: upgrade)
    add("join-node", String(localized: "Add a node"), operate, "plus.circle", .joinNode, when: talos)
    add("updates", String(localized: "Updates"), operate, "arrow.up.square", .updates)
    add("runbooks", menuTitle(String(localized: "Runbooks…")), operate, "list.bullet.rectangle", .runbooks)
    let settings = String(localized: "Settings")
    for page in SettingsPage.visible(kubeconfig: model.allows(.kubeconfig)) {
        specs.append(ScreenSpec(id: "settings/\(page.androidRoute)", title: "\(settings) → \(page.titleString)", section: settings,
                                systemImage: page.systemImage, subtitle: page.summaryString, route: .settingsPage(page)))
    }
    if talos { specs += nodeScreens(model: model, nodes: nodes) }
    return specs
}

/// Each node's screens with a route of their own, as the node hub offers them to the role.
@MainActor
private func nodeScreens(model: AppModel, nodes: [NodeOverview]) -> [ScreenSpec] {
    var seen = Set<String>()
    return nodes.filter { seen.insert($0.node).inserted }.flatMap { node -> [ScreenSpec] in
        let host = node.hostname.isEmpty ? node.node : node.hostname
        let a = node.node
        // The hub's own titles, without the "…" of a row that opens a plan.
        let screens: [(String, String, String, Route, Bool)] = [
            ("kernel_log", String(localized: "Kernel log"), "terminal", .logs(node: a, hostname: host, service: nil), true),
            ("events", String(localized: "Events"), "timeline.selection", .events(node: a, hostnames: [a: host]), true),
            ("hardware", String(localized: "About this node"), "memorychip", .hardware(node: a, hostname: host), true),
            ("storage", String(localized: "Storage"), "internaldrive", .storage(node: a, hostname: host), true),
            ("debug_shell", String(localized: "Debug shell"), "apple.terminal", .debugShell(node: a, hostname: host), model.allows(.debugShell)),
            ("machine_config", String(localized: "Machine config"), "doc.text", .machineConfig(node: a, hostname: host), model.allows(.machineConfig)),
            ("upgrade", menuTitle(String(localized: "Upgrade Talos…")), "arrow.up.circle", .upgrade(node: a, hostname: host), model.allows(.upgrade)),
            ("maintenance", menuTitle(String(localized: "Maintenance…")), "wrench.and.screwdriver", .maintenance(node: a, hostname: host), model.allows(.workloads)),
            ("drain", menuTitle(String(localized: "Drain…")), "rectangle.portrait.and.arrow.right", .drain(node: a, hostname: host), model.allows(.workloads)),
            ("taints_labels", menuTitle(String(localized: "Taints and labels…")), "tag", .nodeMeta(node: a, hostname: host), model.allows(.workloads)),
        ]
        return screens.filter { $0.4 }.map { screen in
            ScreenSpec(id: "node/\(a)/\(screen.0)", title: "\(host) → \(screen.1)", section: host, systemImage: screen.2,
                       keywords: screen.0 == "hardware" ? ["hardware", "sensors", "cpu"] : [], route: screen.3)
        }
    }
}

/// A menu label without its trailing "…".
private func menuTitle(_ label: String) -> String {
    label.hasSuffix("…") ? String(label.dropLast()) : label
}

extension SettingsPage {
    /// The page's name in Android's routes, so both catalogs give a page the same id.
    var androidRoute: String { self == .gitHosts ? "git-hosts" : rawValue }

    /// The title and summary as plain strings, for the Go core to rank (same keys as `title`).
    var titleString: String {
        switch self {
        case .appearance: String(localized: "Appearance")
        case .security: String(localized: "Security")
        case .privacy: String(localized: "Privacy")
        case .alerts: String(localized: "Alerts")
        case .ai: String(localized: "AI diagnosis")
        case .gitHosts: String(localized: "Git hosts")
        case .plugins: String(localized: "Plugins")
        case .kubernetes: "Kubernetes"
        case .clusters: String(localized: "Clusters & backup")
        case .support: String(localized: "Support")
        case .about: String(localized: "About")
        }
    }

    var summaryString: String {
        switch self {
        case .appearance: String(localized: "Theme, language, live usage")
        case .security: String(localized: "App lock, security keys")
        case .privacy: String(localized: "Screenshot mode, cluster events, app icons, last known state")
        case .alerts: String(localized: "Background checks, watches, thresholds")
        case .ai: String(localized: "Provider, API key, model")
        case .gitHosts: String(localized: "A token per Git host, to read and change the repositories Argo CD and Flux deploy from.")
        case .plugins: String(localized: "Built-in and imported CRD plugins")
        case .kubernetes: String(localized: "Node debug shell, kubeconfig export")
        case .clusters: String(localized: "Add, renew, back up, delete")
        case .support: String(localized: "Support bundle, activity log, report a bug")
        case .about: String(localized: "Version, what's new, licenses, updates")
        }
    }
}
