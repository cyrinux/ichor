import AppIntents
import Foundation
import IchorCore

// Compiled into the app and the widget: the widget's settings, its Control Center buttons and
// the app's Shortcuts all pick a cluster with it.

/// A cluster the widget and the shortcuts can name, from the list the app keeps in the App Group
/// (WidgetCluster): named as on screen, so masked while screenshot mode is on.
struct ClusterEntity: AppEntity {
    static let typeDisplayRepresentation: TypeDisplayRepresentation = "Cluster"
    static let defaultQuery = ClusterQuery()

    /// Its monitor key (monitorClusterKey): what its snapshot is kept under.
    let id: String
    let name: String

    var displayRepresentation: DisplayRepresentation { DisplayRepresentation(title: "\(name)") }
}

struct ClusterQuery: EntityQuery {
    private var clusters: [ClusterEntity] {
        SharedSnapshot.clusters(from: UserDefaults(suiteName: SharedSnapshot.suite)).map {
            ClusterEntity(id: $0.id, name: $0.name)
        }
    }

    func entities(for identifiers: [ClusterEntity.ID]) async throws -> [ClusterEntity] {
        clusters.filter { identifiers.contains($0.id) }
    }

    func suggestedEntities() async throws -> [ClusterEntity] { clusters }
}

/// "Open cluster" (Shortcuts, Siri, Control Center): the cluster's overview, once Ichor is
/// unlocked. It only navigates; the app opens the cluster's share link (ClusterScreenHandler).
struct OpenClusterIntent: AppIntent {
    static let title: LocalizedStringResource = "Open cluster"
    static let description = IntentDescription(
        "Opens a cluster's overview in Ichor. None chosen: the one open in the app.")
    static let openAppWhenRun = true

    @Parameter(title: "Cluster")
    var cluster: ClusterEntity?

    init() {}

    @MainActor
    func perform() async throws -> some IntentResult {
        await ClusterScreenHandler.open?(cluster?.id, .cluster)
        return .result()
    }
}

/// "Run a checkup": opens the cluster checkup, which reads the cluster when it appears. Nothing
/// is changed on the cluster.
struct RunCheckupIntent: AppIntent {
    static let title: LocalizedStringResource = "Run a checkup"
    static let description = IntentDescription(
        "Opens the cluster checkup in Ichor. None chosen: the cluster open in the app.")
    static let openAppWhenRun = true

    @Parameter(title: "Cluster")
    var cluster: ClusterEntity?

    init() {}

    @MainActor
    func perform() async throws -> some IntentResult {
        await ClusterScreenHandler.open?(cluster?.id, .checkup)
        return .result()
    }
}
