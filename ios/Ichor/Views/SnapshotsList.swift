import SwiftUI
import IchorCore

/// The VolumeSnapshots, then the claims watched for a recent one, the classes and the contents.
struct SnapshotsList: View {
    let status: VolumeSnapshots

    var body: some View {
        List {
            let errors = [status.error, status.contentsError, status.classesError, status.claimsError].filter {
                !$0.isEmpty
            }
            ForEach(errors, id: \.self) { ErrorLine(error: $0) }
            Section {
                if status.snapshots.isEmpty && status.error.isEmpty {
                    EmptyState(
                        systemImage: "camera",
                        text: String(
                            localized:
                                "No CSI VolumeSnapshots. Longhorn’s own snapshots are under each Longhorn volume."))
                }
                ForEach(status.snapshots) { SnapshotRow(snapshot: $0) }
            }
            let claims = status.claims.filter(\.watched)
            if !claims.isEmpty {
                Section("Watched claims") {
                    ForEach(claims) { SnapshotClaimRow(claim: $0) }
                }
            }
            if !status.classes.isEmpty {
                Section("Snapshot classes") {
                    ForEach(status.classes) { SnapshotClassRow(snapshotClass: $0) }
                }
            }
            if !status.contents.isEmpty {
                Section("Snapshot contents") {
                    ForEach(status.contents) { SnapshotContentRow(content: $0) }
                }
            }
        }
        .themedBackground()
    }
}

/// Critical with an error, a warning while not ready yet.
private func snapshotHealth(ready: Bool, error: String) -> ServiceHealth {
    if !error.isEmpty { return .critical }
    return ready ? .ok : .warning
}

/// "orders · longhorn-snapshot · 3 hours ago", plus pending while it is not ready.
private func snapshotFacts(_ parts: [String], ready: Bool, error: String) -> String {
    let pending = !ready && error.isEmpty ? String(localized: "pending") : ""
    return (parts + [pending]).filter { !$0.isEmpty }.joined(separator: " · ")
}

private struct SnapshotRow: View {
    let snapshot: VolumeSnapshot

    var body: some View {
        ResourceRow(
            health: snapshotHealth(ready: snapshot.ready, error: snapshot.error), title: snapshot.id,
            subtitle: snapshotFacts(
                [snapshot.pvc, snapshot.className, relativeTime(snapshot.createdAt)],
                ready: snapshot.ready, error: snapshot.error)
        ) {
            SnapshotErrorText(error: snapshot.error)
        }
        .accessibilityElement(children: .combine)
    }
}

private struct SnapshotClaimRow: View {
    let claim: SnapshotClaim

    var body: some View {
        ResourceRow(health: claim.stale ? .warning : .ok, title: claim.id, subtitle: subtitle)
            .accessibilityElement(children: .combine)
    }

    private var subtitle: String {
        let stale = String(localized: "no good snapshot in 48 hours")
        guard claim.lastGoodAt > 0 else { return stale }
        let lastGood = String(localized: "last good snapshot \(relativeTime(claim.lastGoodAt))")
        return claim.stale ? lastGood + " · " + stale : lastGood
    }
}

private struct SnapshotClassRow: View {
    let snapshotClass: SnapshotClass

    var body: some View {
        ResourceRow(health: nil, title: snapshotClass.name, subtitle: snapshotClass.driver, trailing: tags)
            .accessibilityElement(children: .combine)
    }

    private var tags: some View {
        HStack(spacing: 4) {
            if snapshotClass.default { Tag(String(localized: "default")) }
            if !snapshotClass.deletionPolicy.isEmpty { Tag(snapshotClass.deletionPolicy) }
        }
    }
}

private struct SnapshotContentRow: View {
    let content: SnapshotContent

    var body: some View {
        let source = content.snapshot.isEmpty ? "" : content.namespace + "/" + content.snapshot
        ResourceRow(
            health: snapshotHealth(ready: content.ready, error: content.error), title: content.name,
            subtitle: snapshotFacts(
                [source, content.className, relativeTime(content.createdAt)],
                ready: content.ready, error: content.error)
        ) {
            SnapshotErrorText(error: content.error)
        }
        .accessibilityElement(children: .combine)
    }
}

/// A snapshot's own error under its row, muted: the dot already says it failed.
private struct SnapshotErrorText: View {
    let error: String

    var body: some View {
        if !error.isEmpty {
            Text(verbatim: error).font(.caption).foregroundStyle(.secondary).lineLimit(3)
        }
    }
}

/// A backup CronJob: the dot says whether it ran well, the subtitle when it last did, a pill
/// counts failed runs in a row, and the schedule reads as words to VoiceOver.
struct BackupJobsList: View {
    let status: BackupJobStatus

    var body: some View {
        List {
            if !status.error.isEmpty { ErrorLine(error: status.error) }
            if !status.jobsError.isEmpty { ErrorLine(error: status.jobsError) }
            if status.jobs.isEmpty && status.error.isEmpty {
                EmptyState(systemImage: "clock.arrow.circlepath", text: String(localized: "No backup CronJobs."))
            }
            let now = Date().epochMillis
            ForEach(status.jobs) { BackupJobRow(job: $0, now: now) }
        }
        .themedBackground()
    }
}

private struct BackupJobRow: View {
    let job: BackupCronJob
    let now: Int64

    var body: some View {
        ResourceRow(health: job.rowHealth(now: now), title: job.id, subtitle: lastSuccess, trailing: trailing) {
            if !job.schedule.isEmpty {
                Tag(job.schedule, mono: true)
                    .accessibilityLabel(Text(verbatim: cronWords(job.schedule)))
            }
        }
    }

    private var lastSuccess: String {
        job.lastSuccess > 0
            ? String(localized: "last success \(relativeTime(job.lastSuccess))") : String(localized: "no success yet")
    }

    @ViewBuilder private var trailing: some View {
        HStack(spacing: 4) {
            if job.failureStreak > 0 {
                StatusPill(label: String(localized: "\(job.failureStreak) failures in a row"), color: .statusBad)
            }
            if job.suspended { Tag(String(localized: "suspended")) }
        }
    }
}

/// "daily at 09:00", "weekly on Monday at 02:30", "monthly on day 1 at 09:00", else the schedule.
func cronWords(_ schedule: String) -> String {
    switch CronDescriber.describe(schedule) {
    case let .daily(hour, minute):
        return String(localized: "daily at \(CronDescription.clock(hour: hour, minute: minute))")
    case let .weekly(weekday, hour, minute):
        let day = Calendar.current.standaloneWeekdaySymbols[weekday - 1]
        return String(localized: "weekly on \(day) at \(CronDescription.clock(hour: hour, minute: minute))")
    case let .monthly(day, hour, minute):
        return String(localized: "monthly on day \(day) at \(CronDescription.clock(hour: hour, minute: minute))")
    case let .raw(text):
        return text
    }
}
