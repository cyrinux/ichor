import SwiftUI
import IchorCore

/// kube-green puts a namespace's workloads to sleep and restores the replicas it saved at the
/// next wake-up: a workload or CronJob row opened there says so, and a scale or a resume warns
/// that kube-green would undo it. The read never blocks a row and never fails it (see
/// `KubeGreenStore`). Mirrors the Android `KubeGreenNote.kt`.

/// The line a row shows when kube-green has its namespace asleep, with the wake-up time (or
/// "until changed by hand"): for a workload whose kind the SleepInfos suspend, or for a CronJob
/// kube-green suspended (then it says so and when it resumes).
struct KubeGreenAsleepLabel: View {
    let status: KubeGreenNamespace
    /// The kind this row is: "Deployment", "StatefulSet" or "CronJob".
    let kind: String
    /// The CronJob is suspended now: its line says kube-green suspended it and when it resumes.
    var suspended = false

    var body: some View {
        if status.asleep && status.sleeps(kind) {
            Label {
                Text(verbatim: suspended ? kubeGreenCronJobSuspendedLine(status) : kubeGreenAsleepLine(status))
            } icon: {
                Image(systemName: "moon.zzz.fill")
            }
            .font(.caption)
            .foregroundStyle(Color.statusWarn)
            .accessibilityElement(children: .combine)
        }
    }
}

/// "Asleep: kube-green has this namespace asleep (SleepInfo staging/nights), wakes up 15 Oct 2026, 07:00".
func kubeGreenAsleepLine(_ status: KubeGreenNamespace) -> String {
    let sleepInfo = (status.asleepInfo ?? status.sleepInfos.first)?.label ?? ""
    if status.filtered {
        return String(
            localized:
                "Asleep: kube-green may have this namespace asleep (SleepInfo \(sleepInfo)), \(kubeGreenWakeLine(status))"
        )
    }
    return String(
        localized: "Asleep: kube-green has this namespace asleep (SleepInfo \(sleepInfo)), \(kubeGreenWakeLine(status))"
    )
}

/// "Suspended by kube-green, resumes 15 Oct 2026, 07:00".
func kubeGreenCronJobSuspendedLine(_ status: KubeGreenNamespace) -> String {
    let resume =
        status.nextWakeUpAt > 0
        ? String(localized: "resumes \(kubeGreenInstant(status.nextWakeUpAt))")
        : String(localized: "until changed by hand")
    return String(localized: "Suspended by kube-green, \(resume)")
}

/// The warning to show before a change kube-green would undo: while the namespace sleeps, or
/// when it sleeps within a day. nil when kube-green will not touch it, or the state is unknown.
func kubeGreenChangeWarning(_ status: KubeGreenNamespace?) -> String? {
    guard let status, let warn = status.warnBeforeChange() else { return nil }
    switch warn {
    case .asleep:
        return status.nextWakeUpAt > 0
            ? String(
                localized:
                    "kube-green has this namespace asleep: this change is undone at the next wake-up, \(kubeGreenWakeLine(status))"
            )
            : String(
                localized:
                    "kube-green has this namespace asleep: this change is undone when the namespace wakes up, with no wake-up scheduled"
            )
    case .sleepsSoon:
        return String(
            localized:
                "kube-green sleeps this namespace at \(kubeGreenInstant(status.nextSleepAt)): a change made now is undone at the next sleep"
        )
    }
}

/// "wakes up 15 Oct 2026, 07:00", or "until changed by hand" with no wake-up scheduled.
private func kubeGreenWakeLine(_ status: KubeGreenNamespace) -> String {
    status.nextWakeUpAt > 0
        ? String(localized: "wakes up \(kubeGreenInstant(status.nextWakeUpAt))")
        : String(localized: "until changed by hand")
}

private func kubeGreenInstant(_ millis: Int64) -> String {
    Date(epochMillis: millis).formatted(date: .abbreviated, time: .shortened)
}
