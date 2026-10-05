import SwiftUI
import IchorCore

/// The CronJobs of the scope's namespace, each with its icon (the ichor.levis.name/icon label, else guessed
/// from its image, else a clock), schedule, next run and recent runs, and Run now: a Job from
/// its template, like `kubectl create job --from`, and Suspend / Resume of its schedule. Tap a
/// row for its runs.
struct CronJobsList: View {
    let list: PagedList<KubeCronJob>
    let control: KubeScopeControl
    let query: String

    @Environment(AppModel.self) private var model
    @State private var confirm: KubeCronJob?
    @State private var triggering: Set<String> = []
    /// Suspend or resume waiting for confirmation.
    @State private var confirmSuspend: KubeCronJob?
    @State private var suspending: Set<String> = []
    @State private var expanded: Set<String> = []
    @State private var resultMessage: String?

    var body: some View {
        KubeListFrame(control: control, list: list, query: query, namespaces: cronJobNamespaces) { load in
            let selected = control.scope.namespace
            let shown = filterCronJobs(load.items, namespace: selected, query: query, sorted: load.done)
            List {
                ForEach(shown) { cronJob in
                    Section {
                        CronJobRow(cronJob: cronJob, showNamespace: selected == nil,
                                   expanded: expanded.contains(cronJob.id),
                                   triggering: triggering.contains(cronJob.id),
                                   suspending: suspending.contains(cronJob.id),
                                   onToggle: { toggle(cronJob) },
                                   onRun: { confirm = cronJob },
                                   onSuspend: { confirmSuspend = cronJob })
                    }
                }
                if load.hasMore && query.isEmpty {
                    Section { LoadMoreRow { list.loadMore(model: model) } }
                }
            }
            .listSectionSpacing(.compact)
            .overlay {
                if shown.isEmpty {
                    if query.isEmpty {
                        ContentUnavailableView("No CronJobs", systemImage: "clock.arrow.circlepath")
                    } else {
                        ContentUnavailableView.search(text: query)
                    }
                }
            }
            .refreshable { await list.refresh(model: model) }
            .themedBackground()
        }
        .confirmationDialog(confirm.map { String(localized: "Run \($0.displayName) now?") } ?? "",
                            isPresented: $confirm.isPresent(),
                            titleVisibility: .visible,
                            presenting: confirm) { cronJob in
            Button("Run") { Task { await trigger(cronJob) } }
            Button("Cancel", role: .cancel) {}
        } message: { cronJob in
            confirmMessage(cronJob)
        }
        .confirmationDialog(confirmSuspend.map { suspendTitle($0) } ?? "",
                            isPresented: $confirmSuspend.isPresent(),
                            titleVisibility: .visible,
                            presenting: confirmSuspend) { cronJob in
            Button(cronJob.suspended ? String(localized: "Resume") : String(localized: "Suspend"),
                   role: cronJob.suspended ? nil : ButtonRole.destructive) {
                Task { await setSuspended(cronJob, !cronJob.suspended) }
            }
            Button("Cancel", role: .cancel) {}
        } message: { cronJob in
            if cronJob.suspended {
                Text("Its schedule starts again in \(cronJob.namespace). Missed runs may start at once, depending on its starting deadline.")
            } else {
                Text("No new run starts on schedule until it is resumed; a run in progress goes on. Run now still works.")
            }
        }
        .messageAlert($resultMessage)
    }

    private func confirmMessage(_ cronJob: KubeCronJob) -> Text {
        var text = Text("A Job is created from its template in \(cronJob.namespace), like kubectl create job --from.")
        if cronJob.active > 0 {
            text = text + Text(verbatim: " ") + Text("A run is already in progress: this starts another one.")
        }
        if cronJob.suspended {
            text = text + Text(verbatim: " ") + Text("The CronJob is suspended: it runs once, its schedule stays off.")
        }
        return text
    }

    private func suspendTitle(_ cronJob: KubeCronJob) -> String {
        cronJob.suspended ? String(localized: "Resume \(cronJob.displayName)?") : String(localized: "Suspend \(cronJob.displayName)?")
    }

    private func setSuspended(_ cronJob: KubeCronJob, _ suspend: Bool) async {
        guard let client = model.client, !suspending.contains(cronJob.id) else { return }
        suspending.insert(cronJob.id)
        defer { suspending.remove(cronJob.id) }
        do {
            try await client.suspendCronJob(cronJob, suspend: suspend)
            resultMessage = suspend ? String(localized: "\(cronJob.displayName) is suspended")
                : String(localized: "\(cronJob.displayName) is resumed")
            await load()
        } catch {
            resultMessage = String(localized: "Could not change \(cronJob.displayName): \(error.localizedDescription)")
        }
    }

    private func toggle(_ cronJob: KubeCronJob) {
        withAnimation(.snappy) {
            if expanded.contains(cronJob.id) {
                expanded.remove(cronJob.id)
            } else {
                expanded.insert(cronJob.id)
            }
        }
    }

    private func load() async {
        await list.refresh(model: model)
    }

    private func trigger(_ cronJob: KubeCronJob) async {
        guard let client = model.client, !triggering.contains(cronJob.id) else { return }
        triggering.insert(cronJob.id)
        defer { triggering.remove(cronJob.id) }
        do {
            let job = try await client.triggerCronJob(cronJob)
            resultMessage = String(localized: "\(job.isEmpty ? cronJob.name : job) started")
            // The new Job shows as running at once.
            await load()
        } catch {
            resultMessage = String(localized: "Could not run \(cronJob.displayName): \(error.localizedDescription)")
        }
    }
}

private struct CronJobRow: View {
    let cronJob: KubeCronJob
    let showNamespace: Bool
    let expanded: Bool
    let triggering: Bool
    let suspending: Bool
    let onToggle: () -> Void
    let onRun: () -> Void
    let onSuspend: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Button(action: onToggle) { header }
                .buttonStyle(.plain)
            if !cronJob.description.isEmpty {
                Text(verbatim: cronJob.description)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            timing
            HStack(alignment: .center) {
                VStack(alignment: .leading, spacing: 4) {
                    RunHistoryStrip(runs: cronJob.runs)
                    lastRun
                }
                Spacer()
                suspendButton
                runButton
            }
            if expanded {
                RunList(runs: cronJob.runs)
                    .transition(.opacity.combined(with: .move(edge: .top)))
            }
        }
        .padding(.vertical, 4)
    }

    private var header: some View {
        HStack(spacing: 12) {
            AppIconView(app: cronJob.iconApp, size: 46, fallbackSymbol: "clock.arrow.circlepath")
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: cronJob.displayName)
                    .font(.headline)
                    .lineLimit(1)
                let subtitle = [cronJob.title.isEmpty ? nil : cronJob.name, showNamespace ? cronJob.namespace : nil].compactMap { $0 }
                if !subtitle.isEmpty {
                    Text(verbatim: subtitle.joined(separator: " · "))
                        .font(.caption.monospaced())
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                        .truncationMode(.middle)
                }
            }
            Spacer(minLength: 4)
            RunStatePill(state: cronJob.runState)
        }
        .contentShape(Rectangle())
    }

    private var timing: some View {
        HStack(spacing: 8) {
            MetaChip(symbol: "clock", text: Text(verbatim: [cronJob.schedule, cronJob.timeZone].filter { !$0.isEmpty }.joined(separator: " · ")), mono: true)
            if cronJob.suspended {
                MetaChip(symbol: "pause.circle", text: Text("suspended"), color: .orange)
            } else if Date(epochMillis: cronJob.nextRun) > .now {
                // Not from cached data that has gone by.
                MetaChip(symbol: "arrow.clockwise", text: Text("next \(Date(epochMillis: cronJob.nextRun).formatted(.relative(presentation: .named)))"))
            }
        }
    }

    @ViewBuilder private var lastRun: some View {
        if let run = cronJob.runs.first {
            let ago = Date(epochMillis: run.started).formatted(.relative(presentation: .named))
            let duration = run.duration.map { Duration.seconds($0).formatted(.units(allowed: [.hours, .minutes, .seconds], width: .narrow)) }
            (Text("last run \(ago)") + Text(verbatim: duration.map { " · \($0)" } ?? ""))
                .font(.caption)
                .foregroundStyle(.secondary)
        } else {
            Text("No run kept.").font(.caption).foregroundStyle(.secondary)
        }
    }

    @ViewBuilder private var suspendButton: some View {
        if suspending {
            ProgressView()
        } else {
            Button(action: onSuspend) {
                Image(systemName: cronJob.suspended ? "play.circle" : "pause.circle")
                    .frame(minWidth: 44, minHeight: 44)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.borderless)
            .accessibilityLabel(cronJob.suspended ? Text("Resume \(cronJob.displayName)") : Text("Suspend \(cronJob.displayName)"))
        }
    }

    @ViewBuilder private var runButton: some View {
        if triggering {
            ProgressView()
        } else if cronJob.triggerable {
            Button(action: onRun) {
                Label("Run now", systemImage: "play.fill")
                    .font(.subheadline.weight(.semibold))
            }
            .buttonStyle(.borderedProminent)
            .buttonBorderShape(.capsule)
            .controlSize(.small)
        } else {
            Label("Schedule only", systemImage: "lock")
                .font(.caption)
                .foregroundStyle(.secondary)
        }
    }
}

private struct MetaChip: View {
    let symbol: String
    let text: Text
    var color: Color = .secondary
    var mono = false

    var body: some View {
        HStack(spacing: 4) {
            Image(systemName: symbol).imageScale(.small)
            text.font(mono ? .caption.monospaced() : .caption).lineLimit(1)
        }
        .foregroundStyle(color)
        .padding(.horizontal, 8)
        .padding(.vertical, 4)
        .background(Color(.tertiarySystemFill), in: RoundedRectangle(cornerRadius: 8, style: .continuous))
    }
}

private struct RunStatePill: View {
    let state: JobRunState

    var body: some View {
        HStack(spacing: 5) {
            Circle().fill(state.color).frame(width: 7, height: 7)
            Text(state.label).font(.caption.weight(.medium))
        }
        .foregroundStyle(state.color)
        .padding(.horizontal, 9)
        .padding(.vertical, 4)
        .background(state.color.opacity(0.15), in: Capsule())
    }
}

/// The recent runs as small bars, oldest left; a manual run is drawn hollow.
private struct RunHistoryStrip: View {
    let runs: [KubeJobRun]

    var body: some View {
        if !runs.isEmpty {
            HStack(spacing: 4) {
                ForEach(runs.reversed()) { run in
                    let shape = RoundedRectangle(cornerRadius: 3, style: .continuous)
                    Group {
                        if run.manual {
                            shape.strokeBorder(run.runState.color, lineWidth: 1.5)
                        } else {
                            shape.fill(run.runState.color)
                        }
                    }
                    .frame(width: 14, height: 8)
                }
            }
            .accessibilityHidden(true)
        }
    }
}

private struct RunList: View {
    let runs: [KubeJobRun]

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Divider()
            Text("Recent runs").font(.subheadline.weight(.semibold))
            if runs.isEmpty {
                Text("No run kept.").font(.footnote).foregroundStyle(.secondary)
            }
            ForEach(runs) { run in
                HStack(spacing: 8) {
                    Circle().fill(run.runState.color).frame(width: 8, height: 8)
                    VStack(alignment: .leading, spacing: 1) {
                        Text(verbatim: run.name)
                            .font(.footnote.monospaced())
                            .lineLimit(1)
                            .truncationMode(.middle)
                        let ago = Date(epochMillis: run.started).formatted(.relative(presentation: .named))
                        let duration = run.duration.map { Duration.seconds($0).formatted(.units(allowed: [.hours, .minutes, .seconds], width: .narrow)) }
                        (Text(run.runState.label) + Text(verbatim: " · \(ago)" + (duration.map { " · \($0)" } ?? "")))
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                    Spacer()
                    if run.manual {
                        Label("manual", systemImage: "hand.tap")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }
            }
        }
    }
}

private extension JobRunState {
    var label: LocalizedStringKey {
        switch self {
        case .running: "running"
        case .succeeded: "succeeded"
        case .failed: "failed"
        case .never: "never run"
        }
    }

    var color: Color {
        switch self {
        case .running: .accentColor
        case .succeeded: .green
        case .failed: .red
        case .never: .secondary
        }
    }
}
