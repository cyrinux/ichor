import Foundation

// The vulnerability scan of a Kubernetes object's images from its object screen: a pod's by
// the images it runs, a workload's by the image refs of its spec (KubeWorkloadsNamed).

/// How an object's images are scanned.
public enum ObjectScanMode: Equatable, Sendable {
    /// A pod: its own images, by digest (the scan's pods).
    case pod
    /// A Deployment, StatefulSet or DaemonSet: the image refs of its spec, read with
    /// KubeWorkloadsNamed (the scan's images). The findings are for those tags.
    case workload
}

/// The workload kinds KubeWorkloadsNamed reads (apps/v1), by resource.
private let scannableWorkloadResources: Set<String> = ["deployments", "statefulsets", "daemonsets"]

extension KubeAPIResource {
    /// How its objects' images are scanned, nil for a kind with no images to scan here.
    public var objectScanMode: ObjectScanMode? {
        if isPod { return .pod }
        if group == "apps", scannableWorkloadResources.contains(resource) { return .workload }
        return nil
    }
}

private let objectScanPrefix = "object:"

/// The scan id an object's report is kept under: "object:<kind>:<namespace>/<name>".
public func objectScanID(kind: String, namespace: String, name: String) -> String {
    "\(objectScanPrefix)\(kind):\(namespace)/\(name)"
}

/// "Deployment shop/web" for an object's scan id, nil for an app's or a node's.
public func objectScanLabel(_ scanID: String) -> String? {
    guard scanID.hasPrefix(objectScanPrefix) else { return nil }
    let rest = scanID.dropFirst(objectScanPrefix.count)
    guard let colon = rest.firstIndex(of: ":") else { return nil }
    let kind = rest[..<colon]
    let object = rest[rest.index(after: colon)...]
    guard !kind.isEmpty, !object.isEmpty else { return nil }
    return "\(kind) \(object)"
}

/// What one scan section scans and keeps its report under: an app's pods, a pod, or a
/// workload's image refs.
public struct ImageScanTarget: Hashable, Sendable {
    /// The id the core keeps the report under (an app id, `objectScanID`).
    public let id: String
    /// What the report is named after (its exports' file names).
    public let name: String
    public let pods: [RoutePod]
    /// Image refs scanned with no pod behind them.
    public let images: [String]
    /// The images are a workload's spec refs, not what runs: the section says so.
    public let byRefs: Bool

    public init(id: String, name: String, pods: [RoutePod] = [], images: [String] = [], byRefs: Bool = false) {
        self.id = id
        self.name = name
        self.pods = pods
        self.images = images
        self.byRefs = byRefs
    }

    /// Something to scan.
    public var isEmpty: Bool { pods.isEmpty && images.isEmpty }

    /// An app's pods.
    public init(app: InventoryApp) {
        self.init(id: app.id, name: app.name, pods: app.routePods)
    }

    /// A pod, by the images it runs.
    public static func pod(namespace: String, name: String) -> ImageScanTarget {
        ImageScanTarget(id: objectScanID(kind: "Pod", namespace: namespace, name: name), name: name,
                        pods: [RoutePod(namespace: namespace, pod: name)])
    }

    /// A workload, by its spec's image refs (once each, in order).
    public static func workload(kind: String, namespace: String, name: String, images: [String]) -> ImageScanTarget {
        var seen = Set<String>()
        let refs = images.filter { !$0.isEmpty && seen.insert($0).inserted }
        return ImageScanTarget(id: objectScanID(kind: kind, namespace: namespace, name: name), name: name,
                               images: refs, byRefs: true)
    }
}
