import SwiftUI
import IchorCore

struct SnapshotsList: View {
    let status: VolumeSnapshots
    var body: some View {
        List {
            ForEach([status.error, status.contentsError, status.classesError, status.claimsError].filter { !$0.isEmpty }, id: \.self) { ErrorLine(error: $0) }
            Section {
                ForEach(status.snapshots) { s in
                    VStack(alignment: .leading, spacing: 4) {
                        Text(verbatim: s.id).font(.callout.monospaced())
                        Text(s.ready ? LocalizedStringKey("Snapshot ready") : LocalizedStringKey("Snapshot pending"))
                        Text("PVC: \(s.pvc) · class: \(s.className)").font(.caption).foregroundStyle(.secondary)
                        Text("Created: \(relativeTime(s.createdAt))").font(.caption)
                        if !s.error.isEmpty { ErrorLine(error: s.error) }
                    }
                }
                if status.snapshots.isEmpty && status.error.isEmpty { Text("No volume snapshots.") }
            }
            Section {
                ForEach(status.claims.filter(\.watched)) { c in
                    VStack(alignment: .leading) {
                        Text(verbatim: c.id)
                        Text("Last good snapshot: \(relativeTime(c.lastGoodAt))").font(.caption)
                        if c.stale { Text("No good snapshot in 48 hours").foregroundStyle(Color.statusWarn) }
                    }
                }
            }
            Section {
                ForEach(status.classes) { c in
                    VStack(alignment: .leading) {
                        Text(verbatim: c.name)
                        Text(verbatim: c.driver + " · " + c.deletionPolicy).font(.caption)
                        if c.default { Text("Default snapshot class").font(.caption) }
                    }
                }
            }
            Section {
                ForEach(status.contents) { c in
                    VStack(alignment: .leading) {
                        Text(verbatim: c.name)
                        Text(verbatim: c.namespace + "/" + c.snapshot).font(.caption)
                        Text(verbatim: c.className + " · " + c.driver).font(.caption)
                        Text("Created: \(relativeTime(c.createdAt))").font(.caption)
                        Text(c.ready ? LocalizedStringKey("Snapshot ready") : LocalizedStringKey("Snapshot pending")).font(.caption)
                        if !c.error.isEmpty { ErrorLine(error: c.error) }
                    }
                }
            }
        }.themedBackground()
    }
}
struct BackupJobsList: View {
    let status: BackupJobStatus
    var body: some View {
        List {
            if !status.error.isEmpty { ErrorLine(error: status.error) }
            if !status.jobsError.isEmpty { ErrorLine(error: status.jobsError) }
            ForEach(status.jobs) { j in
                VStack(alignment: .leading, spacing: 4) {
                    Text(verbatim: j.id).font(.callout.monospaced())
                    Text(verbatim: j.schedule).font(.caption)
                    Text("Last successful backup: \(relativeTime(j.lastSuccess))").font(.caption)
                    Text("Observed consecutive failures: \(j.failureStreak)")
                    if j.suspended { Text("Backup schedule suspended").font(.caption) }
                }
            }
        }.themedBackground()
    }
}
