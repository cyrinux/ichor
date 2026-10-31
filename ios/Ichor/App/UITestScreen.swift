import Foundation
import IchorCore

/// The screens the screenshot walk opens directly (`ICHOR_UI_TEST_SCREEN`, the names of
/// Android's DemoScreenshots and scripts/ci-screenshots.sh), on the demo cluster.
enum UITestScreen {
    struct Failure: LocalizedError {
        let errorDescription: String?
    }

    /// The Argo CD app of the demo that has a source and a diff, as on Android.
    private static let argoApp = (namespace: "argocd", name: "grafana")

    static func route(named name: String, model: AppModel) async throws -> Route {
        if let route = plainRoute(named: name) { return route }
        guard let client = model.client else { throw Failure(errorDescription: "No client for the demo") }
        if name == "gitops" { return try await gitOpsRoute(client) }
        let nodes = try await client.overview().nodes.overviewOrder
        guard let node = nodes.first?.ref else { throw Failure(errorDescription: "The demo has no node") }
        switch name {
        case "node-services": return .nodeTab(node, tab: .services)
        case "logs": return .logs(node: node.address, hostname: node.hostname, service: nil)
        case "events": return .events(node: node.address, hostnames: [node.address: node.hostname])
        case "graphs": return .nodeLive(node)
        case "capture": return .capture(node: node.address, hostname: node.hostname)
        case "cgroups": return .nodeTab(node, tab: .cgroups)
        case "apps":
            let hostnames = nodes.reduce(into: [String: String]()) { $0[$1.node] = $1.hostname }
            return .apps(hostnames: hostnames)
        default: throw Failure(errorDescription: "Unknown ICHOR_UI_TEST_SCREEN value")
        }
    }

    /// The screens that need nothing read from the demo.
    private static func plainRoute(named name: String) -> Route? {
        switch name {
        // "": the newest release the plan screen offers.
        case "upgrade": .clusterUpgrade(version: "")
        case "plugins": .settingsPage(.plugins)
        case "capacity": .capacity
        case "argocd-diff": .argoDiff(namespace: argoApp.namespace, name: argoApp.name)
        case "argocd": .argoCD(downNodes: [])
        case "metrics": .metrics
        case "data-services": .dataServices(hints: "", downNodes: [])
        case "castai": .dataServices(hints: "", downNodes: [], kind: .castai)
        case "workloads": .kubernetes(KubeFocus(tab: .workloads))
        case "pods": .kubernetes(KubeFocus(tab: .pods))
        case "cronjobs": .kubernetes(KubeFocus(tab: .cronJobs))
        // The fourth tab has no KubeFocus: it is the Live Activity's route.
        case "netperf": .networkTest
        case "drift": .insights
        case "import": .importConfig
        default: nil
        }
    }

    private static func gitOpsRoute(_ client: TalosClient) async throws -> Route {
        let read = try await client.gitOpsSource(
            tool: "argocd", kind: "Application", namespace: argoApp.namespace, name: argoApp.name)
        guard let source = read.sources.first else {
            throw Failure(errorDescription: "The demo's \(argoApp.name) app has no source")
        }
        return .gitOpsSource(source, dir: source.path, app: .argo(namespace: argoApp.namespace, name: argoApp.name))
    }
}
