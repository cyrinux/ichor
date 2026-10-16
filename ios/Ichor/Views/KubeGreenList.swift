import SwiftUI
import IchorCore

/// kube-green's sleep schedules, problems first: one row per SleepInfo with whether its
/// namespace is asleep now and when it wakes up (or sleeps again), opening to the details and
/// "Edit schedule".
struct KubeGreenList: View {
    let status: KubeGreenStatus
    let refresh: () async -> Void
    @State private var editing: KubeGreenSleepInfo?

    var body: some View {
        List {
            if !status.error.isEmpty { Section { ErrorLine(error: status.error) } }
            Section {
                if status.sleepInfos.isEmpty {
                    Text("No sleep schedules.").foregroundStyle(.secondary)
                }
                ForEach(status.sleepInfos) { info in
                    SleepInfoRow(info: info) { editing = info }
                }
            }
        }
        .refreshable { await refresh() }
        .themedBackground()
        .sheet(item: $editing) { KubeGreenScheduleSheet(info: $0, onSaved: refresh) }
    }
}

private struct SleepInfoRow: View {
    let info: KubeGreenSleepInfo
    let onEdit: () -> Void

    var body: some View {
        DisclosureGroup {
            details
        } label: {
            HStack(alignment: .top, spacing: 12) {
                HealthDot(health: info.health).padding(.top, 5)
                VStack(alignment: .leading, spacing: 2) {
                    Text(verbatim: info.label).font(.subheadline.monospaced()).lineLimit(1)
                    Text(verbatim: stateLine)
                        .font(.caption)
                        .foregroundStyle(info.health.needsAttention ? info.health.color : .secondary)
                        .lineLimit(2)
                }
            }
        }
    }

    /// "Asleep · wakes up 15 Oct 2026, 07:00", "Awake · sleeps 14 Oct 2026, 20:00",
    /// "Asleep · until changed by hand", then the reasons.
    private var stateLine: String {
        let state = info.asleep ? String(localized: "Asleep") : String(localized: "Awake")
        var parts: [String] = [state]
        if info.asleep {
            parts.append(info.nextWakeUpAt > 0
                ? String(localized: "wakes up \(Date(epochMillis: info.nextWakeUpAt).formatted(date: .abbreviated, time: .shortened))")
                : String(localized: "until changed by hand"))
        } else if info.nextSleepAt > 0 {
            parts.append(String(localized: "sleeps \(Date(epochMillis: info.nextSleepAt).formatted(date: .abbreviated, time: .shortened))"))
        }
        parts += info.reasons.map(reasonText)
        return parts.joined(separator: " · ")
    }

    private func reasonText(_ reason: KubeGreenReason) -> String {
        switch reason {
        case .noWakeUp: String(localized: "no wake-up time")
        case .invalidSchedule: String(localized: "invalid schedule")
        case .overdue: String(localized: "controller missed its last run")
        case .neverRan: String(localized: "never ran yet")
        }
    }

    @ViewBuilder private var details: some View {
        LabeledContent("Weekdays", value: weekdayNames(info.weekdays))
        LabeledContent("Times", value: "\(info.sleepAt) – \(info.wakeUpAt.isEmpty ? "…" : info.wakeUpAt)")
        if !info.timeZone.isEmpty { LabeledContent("Time zone", value: info.timeZone) }
        let suspends = [info.suspendDeployments ? String(localized: "Deployments") : nil,
                        info.suspendStatefulSets ? String(localized: "StatefulSets") : nil,
                        info.suspendCronJobs ? String(localized: "CronJobs") : nil].compactMap { $0 }
        LabeledContent("Suspends", value: suspends.isEmpty ? String(localized: "nothing") : suspends.joined(separator: ", "))
        let scope = [info.excludeCount > 0 ? String(localized: "\(info.excludeCount) excluded") : nil,
                     info.includeCount > 0 ? String(localized: "\(info.includeCount) included") : nil,
                     info.patchCount > 0 ? String(localized: "\(info.patchCount) patches") : nil].compactMap { $0 }
        if !scope.isEmpty { LabeledContent("Scope", value: scope.joined(separator: ", ")) }
        if info.lastScheduleAt > 0 { LabeledContent("Last run", value: relativeTime(info.lastScheduleAt)) }
        KubeGreenEditButton(info: info, onEdit: onEdit)
    }
}

/// "Edit schedule", off with the reason when the credentials may not patch the SleepInfo.
private struct KubeGreenEditButton: View {
    let info: KubeGreenSleepInfo
    let onEdit: () -> Void
    @Environment(AppModel.self) private var model
    @State private var denial: KubeAccess?

    var body: some View {
        Button("Edit schedule", action: onEdit)
            .disabled(denial != nil)
            .task(id: info.id) {
                guard let client = model.client,
                      let access = try? await client.kubeCan(verb: "patch", group: "kube-green.com", resource: "sleepinfos",
                                                             namespace: info.namespace, name: info.name) else { return }
                denial = access.isDenied ? access : nil
            }
        if let denial { KubeDeniedLine(text: denial.deniedText) }
    }
}

/// "Mon – Fri" from the cron day-of-week field ("1-5", "*", "1,3,5").
private func weekdayNames(_ weekdays: String) -> String {
    if weekdays.isEmpty || weekdays == "*" { return "–" }
    let names = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"]

    return weekdays.split(separator: ",").map { part -> String in
        let bounds = part.split(separator: "-").compactMap { Int($0.trimmingCharacters(in: .whitespaces)) }
        switch bounds.count {
        case 1: return names[bounds[0] % 7]
        case 2: return "\(names[bounds[0] % 7]) – \(names[bounds[1] % 7])"
        default: return String(part)
        }
    }.joined(separator: ", ")
}
