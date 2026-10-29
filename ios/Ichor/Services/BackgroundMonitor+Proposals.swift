import Foundation
import IchorCore

extension Notification.Name {
    /// The background monitor saved new states for the proposals (AppModel reloads them).
    static let proposalsChanged = Notification.Name("name.levis.ichor.proposalsChanged")
}

/// A proposal of a cluster that was merged or closed since the last run: one notification.
struct ProposalAlert {
    let context: ContextSummary
    let change: ProposalChange
}

// The pull requests opened from the phone: read again on their forges at each run, so the user
// is told when one is merged or closed (same rules as Android, monitor/ProposalIssues.kt).
extension BackgroundMonitor {
    /// Reads the proposals of `contexts` still open (or never read) on their forges, one call per
    /// host and cluster with the host's saved token (a host without one is skipped), and keeps
    /// their new states; the ones that were open and are now merged or closed come back to be
    /// told. Nothing in the demo, nothing without proposals.
    static func checkProposals(contexts: [ContextSummary], now: Date) async -> [ProposalAlert] {
        var all = ProposalsStore.decode(UserDefaults.standard.data(forKey: ProposalsStore.key))
        guard !all.isEmpty else { return [] }
        let millis = Int64(now.timeIntervalSince1970 * 1000)
        var found: [ProposalAlert] = []
        var changed = false
        for context in contexts where !context.demo {
            guard let records = all[context.fingerprint], !records.isEmpty else { continue }
            var states: [RequestStateItem] = []
            for (host, ask) in proposalsToCheck(records) {
                guard let hostJSON = await MainActor.run(body: { GitHostStore().hostJSON(for: host) }) else { continue }
                states += await TalosClient.requestStates(ask, hostJSON: hostJSON)
            }
            guard !states.isEmpty else { continue }
            let applied = proposalStateChanges(records: records, states: states, now: millis)
            if applied.records != records {
                all[context.fingerprint] = applied.records
                changed = true
            }
            found += applied.changes.map { ProposalAlert(context: context, change: $0) }
        }
        if changed {
            UserDefaults.standard.set(ProposalsStore.encode(all), forKey: ProposalsStore.key)
            await MainActor.run { NotificationCenter.default.post(name: .proposalsChanged, object: nil) }
        }
        return found
    }

    /// The screen a proposal's notification opens: its Argo CD app or Flux object, else the cluster.
    static func proposalTarget(_ record: GitProposalRecord) -> ShareTarget {
        switch record.tool {
        case "argocd": return .argoApp(namespace: record.namespace, name: record.name)
        case "flux": return .fluxApp(kind: record.kind, namespace: record.namespace, name: record.name)
        default: return .screen(.cluster)
        }
    }

    /// "Your pull request #12 was merged", the request's title below (hidden in screenshot mode).
    static func localizedProposal(_ change: ProposalChange, masked: Bool) -> (title: String, text: String) {
        let number = String(change.record.number)
        let title =
            change.state == "merged"
            ? String(localized: "Your pull request #\(number) was merged")
            : String(localized: "Your pull request #\(number) was closed")
        return (title, masked ? "" : change.record.title)
    }
}
