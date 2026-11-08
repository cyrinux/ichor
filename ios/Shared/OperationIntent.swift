import AppIntents
import IchorCore

/// A button on a run's Live Activity (OperationActivityWidget): Stop, Pause, Keep… This file is
/// compiled into the app and the widget extension; as a LiveActivityIntent it is performed in the
/// app's process, where the runs are (OperationActivities sets the handler at launch).
struct OperationIntent: LiveActivityIntent {
    static let title: LocalizedStringResource = "Act on a run"
    static let isDiscoverable = false
    // Never from a locked device: Pause, Revert… change the cluster.
    static let authenticationPolicy: IntentAuthenticationPolicy = .requiresAuthentication

    @Parameter(title: "Run")
    var kind: String

    @Parameter(title: "Button")
    var button: String

    init() {}

    init(kind: OperationKind, button: OperationButton) {
        self.kind = kind.rawValue
        self.button = button.rawValue
    }

    @MainActor
    func perform() async throws -> some IntentResult {
        if let kind = OperationKind(rawValue: kind), let button = OperationButton(rawValue: button) {
            OperationButtonHandler.perform?(kind, button)
        }
        return .result()
    }
}
