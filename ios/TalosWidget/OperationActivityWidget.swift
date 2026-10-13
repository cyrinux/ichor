import ActivityKit
import AppIntents
import IchorCore
import SwiftUI
import WidgetKit

/// A run the app follows (an upgrade, a maintenance, a config try…) on the Lock Screen and in
/// the Dynamic Island: its step, a bar where the core counts, the try's countdown, its buttons,
/// then its result. The app updates it only while it runs: "Updated" says when.
struct OperationActivityWidget: Widget {
    var body: some WidgetConfiguration {
        ActivityConfiguration(for: OperationAttributes.self) { context in
            OperationLockScreenView(kind: context.attributes.kind, content: context.state)
                .padding()
                .widgetURL(context.attributes.kind.link)
        } dynamicIsland: { context in
            let kind = context.attributes.kind
            let content = context.state
            return DynamicIsland {
                DynamicIslandExpandedRegion(.leading) {
                    OperationIcon(kind: kind, content: content).font(.title2)
                }
                DynamicIslandExpandedRegion(.trailing) {
                    OperationCountView(content: content).font(.headline)
                }
                DynamicIslandExpandedRegion(.center) {
                    Text(verbatim: content.title).font(.headline).lineLimit(1)
                }
                DynamicIslandExpandedRegion(.bottom) {
                    VStack(alignment: .leading, spacing: 6) {
                        if !content.step.isEmpty {
                            Text(verbatim: content.step).font(.subheadline).foregroundStyle(.secondary).lineLimit(2)
                        }
                        if content.isDeterminate, !content.phase.ended {
                            ProgressView(value: content.fraction)
                        }
                        OperationButtons(kind: kind, content: content)
                    }
                }
            } compactLeading: {
                OperationIcon(kind: kind, content: content)
            } compactTrailing: {
                OperationCountView(content: content)
            } minimal: {
                OperationIcon(kind: kind, content: content)
            }
            .widgetURL(kind.link)
        }
    }
}

/// The Lock Screen and banner view.
private struct OperationLockScreenView: View {
    let kind: OperationKind
    let content: OperationContent

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 8) {
                OperationIcon(kind: kind, content: content)
                Text(verbatim: content.title).font(.headline).lineLimit(1)
                Spacer(minLength: 4)
                OperationCountView(content: content).font(.headline)
            }
            if !content.step.isEmpty {
                Text(verbatim: content.step)
                    .font(.subheadline)
                    .foregroundStyle(content.phase == .failed ? Color.red : Color.secondary)
                    .lineLimit(2)
            }
            if content.isDeterminate, !content.phase.ended {
                ProgressView(value: content.fraction)
            }
            HStack(spacing: 8) {
                Text("Updated \(content.updated.formatted(date: .omitted, time: .shortened))")
                    .font(.caption2)
                    .foregroundStyle(.secondary)
                Spacer(minLength: 4)
                OperationButtons(kind: kind, content: content)
            }
        }
    }
}

/// The kind's symbol, or how the run ended.
private struct OperationIcon: View {
    let kind: OperationKind
    let content: OperationContent

    var body: some View {
        Image(systemName: symbol)
            .foregroundStyle(tint)
            .accessibilityHidden(true)
    }

    private var symbol: String {
        switch content.phase {
        case .succeeded: "checkmark.circle.fill"
        case .failed: "xmark.octagon.fill"
        case .stopped: "stop.circle"
        case .paused: "pause.circle"
        case .running: kind.symbol
        }
    }

    private var tint: Color {
        switch content.phase {
        case .succeeded: .green
        case .failed: .red
        case .stopped, .paused: .orange
        case .running: .accentColor
        }
    }
}

/// "2/5", or the countdown to a try's revert; nothing otherwise.
private struct OperationCountView: View {
    let content: OperationContent

    var body: some View {
        if let deadline = content.deadline, !content.phase.ended, deadline > Date() {
            Text(timerInterval: Date()...deadline, countsDown: true)
                .monospacedDigit()
                .frame(maxWidth: 64)
        } else if let count = content.count, !content.phase.ended {
            Text(verbatim: count).monospacedDigit()
        }
    }
}

/// The run's buttons. With the app lock on, Stop, Abort and Keep open the app on the run
/// (unlocked first) instead of acting from here, like the run's own screen.
private struct OperationButtons: View {
    let kind: OperationKind
    let content: OperationContent

    var body: some View {
        HStack(spacing: 6) {
            ForEach(content.buttons, id: \.self) { button in
                if button.opensApp(for: kind, locked: content.locked) {
                    Link(destination: kind.link) { Text(button.label) }
                } else {
                    Button(intent: OperationIntent(kind: kind, button: button)) { Text(button.label) }
                }
            }
        }
        .font(.caption)
        .buttonStyle(.bordered)
    }
}

private extension OperationButton {
    var label: LocalizedStringKey {
        switch self {
        case .stop: "Stop"
        case .stopFollowing: "Stop following"
        case .pause: "Pause"
        case .resume: "Resume"
        case .abort: "Abort"
        case .keep: "Keep"
        case .revert: "Revert now"
        }
    }
}

@main
struct IchorWidgets: WidgetBundle {
    var body: some Widget {
        TalosWidget()
        OperationActivityWidget()
    }
}
