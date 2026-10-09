import SwiftUI
import IchorCore

// The freeze sheet and the freeze section of an app page.

/// The hour "until …" freezes to: the start of a working day.
private let morningHour = 9

/// "15:42" today, else with the date.
func freezeClock(_ millis: Int64) -> String {
    let date = Date(epochMillis: millis)
    return Calendar.current.isDateInToday(date)
        ? date.formatted(date: .omitted, time: .shortened)
        : date.formatted(date: .abbreviated, time: .shortened)
}

/// Freezing Argo CD around app: the scope (the app, its namespace, its project), how long, why,
/// and whether manual syncs still run, with the apps it stops named before confirming.
/// pauseInstead offers pausing auto-sync instead, on an app nothing rewrites.
struct ArgoFreezeSheet: View {
    let app: ArgoApp
    let status: ArgoStatus
    let freeze: (ArgoProject, ArgoFreezeOptions) async -> Void
    let pauseInstead: (() async -> Void)?

    @Environment(\.dismiss) private var dismiss
    @State private var scope: FreezeScope
    @State private var minutes = defaultFreezeMinutes
    @State private var untilMorning = false
    @State private var reason = ""
    @State private var manualSync = true
    @State private var working = false
    @State private var now = Date()

    init(app: ArgoApp, status: ArgoStatus, scope: FreezeScope = .app,
         freeze: @escaping (ArgoProject, ArgoFreezeOptions) async -> Void, pauseInstead: (() async -> Void)?) {
        self.app = app
        self.status = status
        self.freeze = freeze
        self.pauseInstead = pauseInstead
        _scope = State(initialValue: scope.available(for: app) ? scope : .app)
    }

    private var chosen: Int { untilMorning ? minutesUntil(hour: morningHour, from: now) : minutes }
    private var targets: [ArgoApp] { status.freezeTargets(around: app, scope: scope) }
    private var endMillis: Int64 { now.epochMillis + Int64(chosen) * 60_000 }

    var body: some View {
        NavigationStack {
            Form {
                if let project = status.project(of: app) {
                    content(project)
                } else {
                    Text("Project \(app.project) cannot be read: there is nothing to freeze.").foregroundStyle(.secondary)
                }
            }
            .navigationTitle(Text("Freeze Argo CD"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    if working {
                        ProgressView()
                    } else if let project = status.project(of: app) {
                        Button(String(localized: "Freeze")) {
                            Task {
                                // "Until 09:00" counts from now, not from when the sheet opened.
                                now = Date()
                                working = true
                                await freeze(project, freezeOptions(for: app, scope: scope, minutes: chosen, manualSync: manualSync, reason: reason))
                                working = false
                                dismiss()
                            }
                        }
                        .fontWeight(.semibold)
                        .disabled(targets.isEmpty)
                    }
                }
            }
            .interactiveDismissDisabled(working)
        }
        .presentationDetents([.medium, .large])
        .presentationDragIndicator(.visible)
    }

    @ViewBuilder
    private func content(_ project: ArgoProject) -> some View {
        Section("Scope") {
            Picker(selection: $scope) {
                ForEach(FreezeScope.allCases.filter { $0.available(for: app) }) { Text(label($0)).tag($0) }
            } label: {
                EmptyView()
            }
            .pickerStyle(.inline)
            .labelsHidden()
        }
        Section("For") {
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 8) {
                    ForEach(freezeDurations, id: \.self) { m in
                        DurationChip(label: durationLabel(m), selected: !untilMorning && minutes == m) {
                            minutes = m
                            untilMorning = false
                        }
                    }
                    let morning = now.epochMillis + Int64(minutesUntil(hour: morningHour, from: now)) * 60_000
                    DurationChip(label: String(localized: "Until \(freezeClock(morning))"), selected: untilMorning) { untilMorning = true }
                }
            }
        }
        Section {
            TextField(String(localized: "Reason (optional)"), text: $reason)
                .onChange(of: reason) { _, text in if text.count > 200 { reason = String(text.prefix(200)) } }
            Toggle(isOn: $manualSync) {
                Text("Allow manual syncs")
                Text("A sync started on purpose still runs during the freeze.")
            }
        } footer: {
            VStack(alignment: .leading, spacing: 6) {
                Text("Argo CD will not auto-sync or self-heal \(targets.count) apps (\(names)) until \(freezeClock(endMillis)). Changes made by hand stay until then.")
                if project.managedServerSide {
                    Label(String(localized: "Project \(project.name) is applied from Git by \(project.managedBy) with server-side apply: the freeze may not survive a sync of \(project.managedBy)."),
                          systemImage: "exclamationmark.triangle")
                        .foregroundStyle(attentionColor)
                } else if !project.managedBy.isEmpty {
                    Text("Project \(project.name) is applied from Git by \(project.managedBy). The freeze is not in Git: it lasts until it ends.")
                }
            }
        }
        if let pauseInstead {
            Section {
                Button(String(localized: "Pause auto-sync instead (until resumed)")) {
                    Task {
                        working = true
                        await pauseInstead()
                        working = false
                        dismiss()
                    }
                }
            }
        }
    }

    private var names: String {
        targets.prefix(6).map(\.name).joined(separator: ", ") + (targets.count > 6 ? ", …" : "")
    }

    private func label(_ scope: FreezeScope) -> String {
        switch scope {
        case .app: String(localized: "This app")
        case .namespace: String(localized: "Namespace \(app.destination.namespace)")
        case .project: String(localized: "Project \(app.project)")
        }
    }

    private func durationLabel(_ minutes: Int) -> String {
        minutes % 60 == 0 ? String(localized: "\(minutes / 60) h") : String(localized: "\(minutes) min")
    }
}

/// A frozen app: until when, by whom, whether manual syncs run, what was changed by hand, with
/// "+1 h" and "Unfreeze" on Ichor's freezes and the sync windows otherwise; "Freeze…" when not.
struct ArgoFreezeSection: View {
    let app: ArgoApp
    let windows: [ArgoWindow]
    let busy: Bool
    let freeze: () -> Void
    let extend: () -> Void
    let unfreeze: () -> Void

    var body: some View {
        Section {
            if let frozen = app.freeze {
                VStack(alignment: .leading, spacing: 4) {
                    Label(String(localized: "Frozen until \(freezeClock(frozen.until))"), systemImage: "snowflake")
                        .font(.headline)
                        .foregroundStyle(.blue)
                    Text(verbatim: detail(frozen)).font(.caption).foregroundStyle(.secondary)
                    if !app.drifted.isEmpty {
                        Text("\(app.drifted.count) resources changed by hand").font(.caption)
                    }
                }
                if frozen.byIchor {
                    HStack {
                        Button(String(localized: "+1 h"), action: extend).buttonStyle(.bordered)
                        Button(String(localized: "Unfreeze"), action: unfreeze).buttonStyle(.borderedProminent)
                    }
                    .disabled(busy)
                } else {
                    NavigationLink(value: ArgoWindowsRoute()) { Text("Sync windows") }
                }
            } else {
                Button(action: freeze) {
                    Label(String(localized: "Freeze auto-sync…"), systemImage: "snowflake")
                }
                .disabled(busy)
            }
        }
    }

    private func detail(_ frozen: ArgoFreeze) -> String {
        let reason = windows.compactMap { $0.ichor?.reason }.filter { !$0.isEmpty }
        return ([frozen.byIchor ? String(localized: "Frozen from Ichor") : String(localized: "A sync window of project \(frozen.project)")]
            + reason
            + [frozen.manualSync ? String(localized: "manual syncs allowed") : String(localized: "manual syncs blocked")])
            .joined(separator: " · ")
    }
}

/// A duration of the freeze sheet: filled when chosen.
private struct DurationChip: View {
    let label: String
    let selected: Bool
    let action: () -> Void

    var body: some View {
        Button(action: action) { Text(verbatim: label) }
            .buttonStyle(.bordered)
            .tint(selected ? .accentColor : .secondary)
            .accessibilityAddTraits(selected ? .isSelected : [])
    }
}

/// The sync windows screen, pushed from the Argo CD screens.
struct ArgoWindowsRoute: Hashable {}
