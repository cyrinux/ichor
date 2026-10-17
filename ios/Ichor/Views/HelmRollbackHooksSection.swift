import SwiftUI
import IchorCore

/// The chart's pre- and post-rollback hooks, in Helm's order, each with the dry run's error
/// when the API server refused to create it (a refused hook keeps the rollback from running).
struct HelmRollbackHooksSection: View {
    let hooks: [HelmRollbackHook]

    var body: some View {
        Section {
            ForEach(hooks) { HelmRollbackHookRow(hook: $0) }
        } header: {
            Text("Hooks")
        }
    }
}

/// "Runs before: Job site-migrate (weight -5)", and the dry run's error below.
private struct HelmRollbackHookRow: View {
    let hook: HelmRollbackHook

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            title.font(.callout)
            if !hook.error.isEmpty {
                Text(verbatim: hook.error).font(.caption).foregroundStyle(Color.statusBad)
            }
        }
        .accessibilityElement(children: .combine)
    }

    private var title: Text {
        let label = hook.label { String(localized: "weight \($0)") }
        return hook.runsBefore ? Text("Runs before: \(label)") : Text("Runs after: \(label)")
    }
}
