import SwiftUI
import IchorCore

/// The vulnerability scan of an object's images on its screen (os:admin): a pod by the images it
/// runs, a Deployment, StatefulSet or DaemonSet by its spec's image refs, read here with
/// KubeWorkloadsNamed. The report is kept under the object (`objectScanID`). The names on screen
/// may be masked (screenshot mode): the core reveals the pods' and workloads' names it is given
/// back, as for an app's pods.
struct KubeObjectScanSection: View {
    let mode: ObjectScanMode
    let kind: String
    let namespace: String
    let name: String

    @Environment(AppModel.self) private var model
    /// The workload's spec refs; loaded at once for a pod (nothing to read).
    @State private var images: LoadState<[String]> = .loading

    private var target: ImageScanTarget {
        switch mode {
        case .pod:
            return .pod(namespace: namespace, name: name)
        case .workload:
            let refs: [String]
            if case .loaded(let value, _, _) = images { refs = value } else { refs = [] }
            return .workload(kind: kind, namespace: namespace, name: name, images: refs)
        }
    }

    private var targetError: String? {
        if case .failed(let message) = images { return message }
        return nil
    }

    var body: some View {
        AppScanSection(target: target, targetError: targetError)
            .task(id: "\(model.activeContext)|\(kind)|\(namespace)/\(name)") { await loadImages() }
    }

    /// A workload's images as its spec has them now; none when it was deleted since.
    private func loadImages() async {
        guard mode == .workload, let client = model.client else { return }
        let ref = WorkloadRef(kind: kind, namespace: namespace, name: name)
        let result: LoadState<[String]> = await .from {
            try await client.workloadsNamed([ref]).first?.images ?? []
        }
        guard !Task.isCancelled else { return }
        images = result
    }
}
