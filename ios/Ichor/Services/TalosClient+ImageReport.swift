import Foundation
import Ichorgo
import IchorCore

extension TalosClient {
    /// The images report of `inventory`, what the Apps screen holds: no second listing. On a
    /// Talos cluster each node's CRI images (as the Images screen reads them) say which images no
    /// container uses; a node that does not answer is left out of that part.
    func imageReport(inventory: ClusterInventory) async throws -> ImageReport {
        let input = try inventory.imageReportInput()
        if kubeOnly {
            return try await Self.json { IchorgoImageReport(input, $0) }
        }
        let byNode = await nodeImages(inventory.reportNodes)
        let nodes = try imageReportNodeImages(byNode)
        return try await Self.json { IchorgoImageReportWithNodeImages(input, nodes, $0) }
    }

    /// The CRI images of each of `nodes`, in parallel; the nodes that fail are left out.
    private func nodeImages(_ nodes: [String]) async -> [String: [ContainerImage]] {
        await withTaskGroup(of: (String, [ContainerImage]?).self) { group in
            for node in nodes {
                group.addTask {
                    (node, try? await self.images(node: node))
                }
            }
            var byNode: [String: [ContainerImage]] = [:]
            for await (node, images) in group {
                if let images { byNode[node] = images }
            }
            return byNode
        }
    }
}
