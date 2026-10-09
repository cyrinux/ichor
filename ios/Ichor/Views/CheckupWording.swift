import SwiftUI
import IchorCore

// How the checkup words what Go found: the same sentences as Android's CheckupWording.kt.

/// How long ago `since` (unix ms) was at `now`, "" when unknown.
func checkupAge(_ since: Int64, now: Int64) -> String {
    since > 0 ? localizedDuration(max(0, (now - since) / 1000)) : ""
}

/// "95%" or "96.5%".
func checkupPercent(_ value: Double) -> String {
    value == value.rounded() ? "\(Int(value))%" : String(format: "%.1f%%", value)
}

private func checkupResource(_ resource: String) -> String {
    resource == "memory" ? CheckupText.checkupMemory : CheckupText.checkupCpu
}

extension CheckupSectionID {
    var title: String {
        switch self {
        case .workloads: CheckupText.checkupSectionWorkloads
        case .events: CheckupText.checkupSectionEvents
        case .storage: CheckupText.checkupSectionStorage
        case .upgrade: CheckupText.checkupSectionUpgrade
        case .webhooks: CheckupText.checkupSectionWebhooks
        case .capacity: CheckupText.checkupSectionCapacity
        case .nodes: CheckupText.checkupSectionNodes
        case .loadbalancers: CheckupText.checkupSectionLoadbalancers
        case .terminating: CheckupText.checkupSectionTerminating
        case .certificates: CheckupText.checkupSectionCertificates
        case .secrets: CheckupText.checkupSectionSecrets
        case .helm: CheckupText.checkupSectionHelm
        }
    }

    /// What the section looks for.
    var hint: String {
        switch self {
        case .workloads: CheckupText.checkupSectionWorkloadsHint
        case .events: CheckupText.checkupSectionEventsHint
        case .storage: CheckupText.checkupSectionStorageHint
        case .upgrade: CheckupText.checkupSectionUpgradeHint
        case .webhooks: CheckupText.checkupSectionWebhooksHint
        case .capacity: CheckupText.checkupSectionCapacityHint
        case .nodes: CheckupText.checkupSectionNodesHint
        case .loadbalancers: CheckupText.checkupSectionLoadbalancersHint
        case .terminating: CheckupText.checkupSectionTerminatingHint
        case .certificates: CheckupText.checkupSectionCertificatesHint
        case .secrets: CheckupText.checkupSectionSecretsHint
        case .helm: CheckupText.checkupSectionHelmHint
        }
    }

    var symbol: String {
        switch self {
        case .workloads: "square.stack.3d.up"
        case .events: "bell.badge"
        case .storage: "externaldrive"
        case .upgrade: "arrow.up.circle"
        case .webhooks: "arrow.triangle.branch"
        case .capacity: "gauge.with.dots.needle.67percent"
        case .nodes: "server.rack"
        case .loadbalancers: "network"
        case .terminating: "trash"
        case .certificates: "checkmark.seal"
        case .secrets: "key"
        case .helm: "shippingbox"
        }
    }
}

extension CheckupStatus {
    var color: Color {
        switch self {
        case .critical: Color.statusBad
        case .warning: Color.statusWarn
        case .ok: Color.statusOK
        case .unknown, .absent: .secondary
        }
    }
}

extension CheckupSeverity {
    var symbol: String {
        switch self {
        case .critical: "exclamationmark.octagon.fill"
        case .warning: "exclamationmark.triangle.fill"
        case .info: "info.circle"
        }
    }

    var color: Color {
        switch self {
        case .critical: Color.statusBad
        case .warning: Color.statusWarn
        case .info: .secondary
        }
    }
}

extension CheckupFinding {
    /// What the finding says happens, in the user's words; the kind's name for one this version
    /// does not know.
    func title(now: Int64) -> String {
        let f = self
        let age = checkupAge(f.since, now: now)
        let limit = "\(Int(f.limit.rounded()))"
        guard let kind else { return kindName }
        switch kind {
        case .podCrashLoop: return CheckupText.checkupPodCrashLoop("\(f.count)")
        case .podImagePull: return CheckupText.checkupPodImagePull(f.extra)
        case .podUnschedulable: return CheckupText.checkupPodUnschedulable(age)
        case .podStuckStarting: return CheckupText.checkupPodStuckStarting(age)
        case .podNotReady: return CheckupText.checkupPodNotReady("\(f.count)", limit)
        case .podFailed: return CheckupText.checkupPodFailed(f.reason)
        case .podOOMKilled: return CheckupText.checkupPodOOMKilled("\(f.count)")
        case .jobFailed: return CheckupText.checkupJobFailed(age)
        case .event: return CheckupText.checkupEvent(f.reason, "\(f.count)", age)
        case .volumeFull: return CheckupText.checkupVolumeFull(checkupPercent(f.value), formatBytes(Int64(f.limit)))
        case .volumeInodes: return CheckupText.checkupVolumeInodes(checkupPercent(f.value))
        case .pvcPending: return CheckupText.checkupPvcPending(age, f.extra.isEmpty ? "-" : f.extra)
        case .pvcLost: return CheckupText.checkupPvcLost
        case .pvFailed: return CheckupText.checkupPvFailed
        case .pvReleased: return CheckupText.checkupPvReleased(f.extra)
        case .deprecatedAPI:
            return f.extra.isEmpty ? CheckupText.checkupDeprecatedAPIUnplanned : CheckupText.checkupDeprecatedAPI(f.extra)
        case .webhookDown, .webhookSkipped: return CheckupText.checkupWebhook(f.reason, f.extra)
        case .nodeRequestsHigh: return CheckupText.checkupNodeRequestsHigh(checkupPercent(f.value), checkupResource(f.extra))
        case .nodePodsFull: return CheckupText.checkupNodePodsFull("\(f.count)", limit)
        case .noRoomToDrain: return CheckupText.checkupNoRoomToDrain(checkupResource(f.extra))
        case .quotaNearLimit: return CheckupText.checkupQuotaNearLimit(checkupPercent(f.value), f.extra)
        case .nodeNotReady: return CheckupText.checkupNodeNotReady
        case .nodePressure: return CheckupText.checkupNodePressure(f.extra)
        case .nodeCordoned: return age.isEmpty ? CheckupText.checkupNodeCordonedPlain : CheckupText.checkupNodeCordoned(age)
        case .nodeVersionSkew: return CheckupText.checkupNodeVersionSkew(f.extra, f.reason)
        case .lbPending: return CheckupText.checkupLbPending(age)
        case .lbPoolExhausted: return CheckupText.checkupLbPoolExhausted("\(Int(f.value.rounded()))")
        case .lbPoolConflict: return CheckupText.checkupLbPoolConflict
        case .namespaceTerminating, .podTerminating, .pvcTerminating: return CheckupText.checkupTerminating(age)
        case .netperfLeftover: return CheckupText.checkupNetperfLeftover(age)
        case .csrPending: return CheckupText.checkupCsrPending("\(f.count)", f.extra)
        case .csrDenied: return CheckupText.checkupCsrDenied("\(f.count)", f.extra)
        case .externalSecretFailed: return CheckupText.checkupExternalSecretFailed(f.extra)
        case .secretStoreNotReady: return CheckupText.checkupSecretStoreNotReady(f.extra)
        case .helmFailed: return CheckupText.checkupHelmFailed("\(f.count)")
        case .helmPending: return CheckupText.checkupHelmPending(f.reason, age)
        }
    }

    /// What to do about it; nil for an event, or a kind this version does not know.
    var fix: String? {
        guard let kind else { return nil }
        switch kind {
        case .podCrashLoop: return CheckupText.checkupPodCrashLoopFix
        case .podImagePull: return CheckupText.checkupPodImagePullFix
        case .podUnschedulable: return CheckupText.checkupPodUnschedulableFix
        case .podStuckStarting: return CheckupText.checkupPodStuckStartingFix
        case .podNotReady: return CheckupText.checkupPodNotReadyFix
        case .podFailed: return CheckupText.checkupPodFailedFix
        case .podOOMKilled: return CheckupText.checkupPodOOMKilledFix
        case .jobFailed: return CheckupText.checkupJobFailedFix
        case .event: return nil
        case .volumeFull: return CheckupText.checkupVolumeFullFix
        case .volumeInodes: return CheckupText.checkupVolumeInodesFix
        case .pvcPending: return CheckupText.checkupPvcPendingFix
        case .pvcLost: return CheckupText.checkupPvcLostFix
        case .pvFailed: return CheckupText.checkupPvFailedFix
        case .pvReleased: return CheckupText.checkupPvReleasedFix
        case .deprecatedAPI: return CheckupText.checkupDeprecatedAPIFix
        case .webhookDown: return CheckupText.checkupWebhookDownFix
        case .webhookSkipped: return CheckupText.checkupWebhookSkippedFix
        case .nodeRequestsHigh: return CheckupText.checkupNodeRequestsHighFix
        case .nodePodsFull: return CheckupText.checkupNodePodsFullFix
        case .noRoomToDrain: return CheckupText.checkupNoRoomToDrainFix
        case .quotaNearLimit: return CheckupText.checkupQuotaNearLimitFix
        case .nodeNotReady: return CheckupText.checkupNodeNotReadyFix
        case .nodePressure: return CheckupText.checkupNodePressureFix
        case .nodeCordoned: return CheckupText.checkupNodeCordonedFix
        case .nodeVersionSkew: return CheckupText.checkupNodeVersionSkewFix
        case .lbPending: return CheckupText.checkupLbPendingFix
        case .lbPoolExhausted: return CheckupText.checkupLbPoolExhaustedFix
        case .lbPoolConflict: return CheckupText.checkupLbPoolConflictFix
        case .namespaceTerminating: return CheckupText.checkupNamespaceTerminatingFix
        case .podTerminating: return CheckupText.checkupPodTerminatingFix
        case .pvcTerminating: return CheckupText.checkupPvcTerminatingFix
        case .netperfLeftover: return CheckupText.checkupNetperfLeftoverFix
        case .csrPending: return CheckupText.checkupCsrPendingFix
        case .csrDenied: return CheckupText.checkupCsrDeniedFix
        case .externalSecretFailed: return CheckupText.checkupExternalSecretFailedFix
        case .secretStoreNotReady: return CheckupText.checkupSecretStoreNotReadyFix
        case .helmFailed: return CheckupText.checkupHelmFailedFix
        case .helmPending: return CheckupText.checkupHelmPendingFix
        }
    }
}
