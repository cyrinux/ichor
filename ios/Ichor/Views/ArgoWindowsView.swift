import SwiftUI
import IchorCore

/// Every sync window of every project: the active ones (Ichor's freezes with "+1 h" and "End"),
/// the upcoming ones, and Ichor's ended freezes. A window Ichor did not create can be removed
/// after a warning: Git may put it back.
struct ArgoWindowsView: View {
    @Environment(AppModel.self) private var model
    @State private var state: LoadState<ArgoStatus> = .loading
    @State private var ending: ProjectWindow?
    @State private var removing: ProjectWindow?
    @State private var message: String?
    @State private var succeeded = 0

    private var store: ArgoCDStore { .shared }

    var body: some View {
        LoadStateView(state: state, retry: load) { status in
            let sections = status.windowSections()
            if sections.values.allSatisfy(\.isEmpty) {
                ContentUnavailableView("No sync windows", systemImage: "calendar.badge.clock",
                                       description: Text("Argo CD may sync at any time."))
                    .themedBackground()
            } else {
                List {
                    ForEach(WindowSection.allCases) { section in
                        let list = sections[section] ?? []
                        if !list.isEmpty {
                            Section {
                                ForEach(Array(list.enumerated()), id: \.offset) { _, pw in row(pw, section: section) }
                            } header: {
                                HStack {
                                    Text(title(section))
                                    Spacer()
                                    if section == .expired {
                                        Button(String(localized: "Clear")) { Task { await clear(list) } }
                                            .font(.caption.weight(.semibold))
                                            .textCase(nil)
                                    }
                                }
                            }
                        }
                    }
                }
                .refreshable { await load() }
                .themedBackground()
            }
        }
        .task(id: model.argoKey) {
            await load()
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(30))
                if Task.isCancelled { return }
                await load()
            }
        }
        .navigationTitle(Text("Sync windows"))
        .navigationBarTitleDisplayMode(.inline)
        .confirmationDialog(String(localized: "End the freeze?"), isPresented: $ending.isPresent(), titleVisibility: .visible, presenting: ending) { pw in
            Button(String(localized: "End")) { Task { await change(.unfreeze, pw, ArgoFreezeOptions(window: pw.window.id)) } }
            Button("Cancel", role: .cancel) {}
        } message: { pw in
            Text(pw.window.apps > 1 ? String(localized: "This freeze covers \(pw.window.apps) apps: they all resume.")
                 : String(localized: "Argo CD resumes auto-sync and self-heal for the apps of this freeze."))
        }
        .confirmationDialog(String(localized: "Remove this sync window?"), isPresented: $removing.isPresent(), titleVisibility: .visible, presenting: removing) { pw in
            Button(String(localized: "Remove"), role: .destructive) {
                Task { await change(.unfreeze, pw, ArgoFreezeOptions(window: pw.window.id, fromGit: true)) }
            }
            Button("Cancel", role: .cancel) {}
        } message: { pw in
            Text(pw.project.managedBy.isEmpty
                 ? String(localized: "Ichor did not create this window. Remove it from project \(pw.project.name)?")
                 : String(localized: "Ichor did not create this window: it likely comes from Git. Argo CD may put it back on the next sync of \(pw.project.managedBy), so remove it in Git too."))
        }
        .messageAlert($message)
        .sensoryFeedback(.success, trigger: succeeded)
    }

    private func title(_ section: WindowSection) -> String {
        switch section {
        case .active: String(localized: "Active")
        case .upcoming: String(localized: "Upcoming")
        case .expired: String(localized: "Ended freezes")
        }
    }

    private func row(_ pw: ProjectWindow, section: WindowSection) -> some View {
        let w = pw.window
        let busy = store.busyProjects.contains(pw.project.id)
        return VStack(alignment: .leading, spacing: 4) {
            Label {
                Text(verbatim: "\(pw.project.name) · \(scope(w))").font(.callout.weight(.medium))
            } icon: {
                Image(systemName: !w.isDeny ? "calendar.badge.checkmark" : w.ichor != nil ? "snowflake" : "nosign")
                    .foregroundStyle(!w.isDeny ? Color.green : w.ichor != nil ? Color.blue : attentionColor)
                    .accessibilityLabel(w.isDeny ? Text("Deny window") : Text("Allow window"))
            }
            Text(verbatim: origin(w)).font(.caption).foregroundStyle(.secondary)
            when(w, section: section)
            Text(verbatim: ([String(localized: "\(w.apps) apps")]
                    + (w.isDeny ? [w.manualSync ? String(localized: "manual syncs allowed") : String(localized: "manual syncs blocked")] : []))
                .joined(separator: " · "))
                .font(.caption)
                .foregroundStyle(.secondary)
            HStack {
                if w.ichor != nil && section == .active {
                    Button(String(localized: "+1 h")) {
                        Task { await change(.extend, pw, ArgoFreezeOptions(minutes: freezeExtendMinutes, window: w.id)) }
                    }
                    .buttonStyle(.bordered)
                    Button(String(localized: "Unfreeze")) { ending = pw }.buttonStyle(.borderedProminent)
                } else if w.ichor == nil {
                    Button(String(localized: "Remove"), role: .destructive) { removing = pw }.buttonStyle(.bordered)
                }
            }
            .disabled(busy)
            .padding(.top, 2)
        }
        .padding(.vertical, 2)
    }

    /// "Ichor freeze · hotfix", or "Not from Ichor · 0 22 * * * 8h Europe/Paris".
    private func origin(_ w: ArgoWindow) -> String {
        if let ichor = w.ichor {
            return ([String(localized: "Ichor freeze")] + (ichor.reason.isEmpty ? [] : [ichor.reason])).joined(separator: " · ")
        }
        return ([String(localized: "Not from Ichor")] + [[w.schedule, w.duration, w.timeZone].filter { !$0.isEmpty }.joined(separator: " ")])
            .joined(separator: " · ")
    }

    /// "38 min left" over a bar while active, "Starts 22:00" before, "Ended 3 h ago" after.
    @ViewBuilder
    private func when(_ w: ArgoWindow, section: WindowSection) -> some View {
        let now = Date().epochMillis
        if !w.error.isEmpty {
            Text("Unreadable: \(w.error)").font(.caption).foregroundStyle(.statusBad)
        } else if section == .expired {
            Text("Ended \(relativeTime(w.endsAt))").font(.caption).foregroundStyle(.secondary)
        } else if section == .active {
            let left = Duration.seconds(max(0, w.endsAt - now) / 1000).formatted(.units(allowed: [.days, .hours, .minutes], width: .abbreviated))
            Text("\(left) left").font(.caption).monospacedDigit()
            ProgressView(value: Double(min(max(now - w.start, 0), max(w.endsAt - w.start, 1))), total: Double(max(w.endsAt - w.start, 1)))
        } else if w.start > 0 {
            Text("Starts \(freezeClock(w.start))").font(.caption).foregroundStyle(.secondary)
        }
    }

    /// "namespace demo", "web-api, worker", "all apps".
    private func scope(_ w: ArgoWindow) -> String {
        var parts: [String] = []
        if !w.applications.isEmpty {
            parts.append(w.applications == ["*"] ? String(localized: "all apps") : w.applications.joined(separator: ", "))
        }
        if !w.namespaces.isEmpty { parts.append(String(localized: "namespace \(w.namespaces.joined(separator: ", "))")) }
        if !w.clusters.isEmpty { parts.append(String(localized: "cluster \(w.clusters.joined(separator: ", "))")) }
        // A window without selectors matches no app.
        return parts.isEmpty ? "—" : parts.joined(separator: " · ")
    }

    private func load() async {
        guard let client = model.client else { return }
        let key = model.argoKey
        let cluster = model.activeSummary, store = self.store
        await store.refresh($state, key: key, currentKey: { model.argoKey }) {
            try await store.load(with: client, key: key, cluster: cluster)
        }
    }

    private func change(_ action: ArgoFreezeAction, _ pw: ProjectWindow, _ options: ArgoFreezeOptions) async {
        guard let client = model.client else { return }
        let failure = await store.freeze(action, on: pw.project, options: [options], with: client)
        if recordActionOutcome(failure, message: &message, succeeded: &succeeded) { announce(String(localized: "Done")) }
        await load()
    }

    private func clear(_ list: [ProjectWindow]) async {
        guard let client = model.client else { return }
        var seen: Set<String> = []
        for pw in list where seen.insert(pw.project.id).inserted {
            if let failure = await store.freeze(.clearExpired, on: pw.project, options: [ArgoFreezeOptions()], with: client) { message = failure }
        }
        await load()
    }
}
