import AppIntents
import SwiftUI
import WidgetKit

// Control Center (iOS 18): buttons that open Ichor on a cluster's overview or its checkup. Their
// intents (Shared/ClusterEntity.swift) run in the app, which opens the screen once unlocked;
// nothing is changed on the cluster.

@available(iOS 18.0, *)
struct OpenClusterControl: ControlWidget {
    var body: some ControlWidgetConfiguration {
        StaticControlConfiguration(kind: "name.levis.ichor.control.open-cluster") {
            ControlWidgetButton(action: OpenClusterIntent()) {
                Label("Open cluster", systemImage: "rectangle.stack")
            }
        }
        .displayName("Open cluster")
        .description("Opens the cluster open in Ichor.")
    }
}

@available(iOS 18.0, *)
struct RunCheckupControl: ControlWidget {
    var body: some ControlWidgetConfiguration {
        StaticControlConfiguration(kind: "name.levis.ichor.control.checkup") {
            ControlWidgetButton(action: RunCheckupIntent()) {
                Label("Run a checkup", systemImage: "stethoscope")
            }
        }
        .displayName("Run a checkup")
        .description("Opens the checkup of the cluster open in Ichor.")
    }
}
