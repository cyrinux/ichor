import SwiftUI
import IchorCore

// Generated from the same English strings as Android's audit_* resources: each key must
// match Localizable.xcstrings exactly.

extension AuditFinding {
    /// What the finding says happens, in the user's words; the kind's name for one this
    /// version does not know.
    var title: String {
        let f = self
        switch kind {
        case .throttled: return String(localized: "Throttled \(f.count) times (429): it sends more than its share")
        case .hotClient: return String(localized: "\(Int(f.value.rounded()))% of all requests, mostly \(f.verb) \(f.resource)")
        case .listLoop: return String(localized: "Lists \(f.target) every \(formatSeconds(f.value))")
        case .watchChurn: return String(localized: "Its watches end after \(formatSeconds(f.value)) and start over")
        case .forbidden: return String(localized: "Refused \(f.count) times (403): \(f.verb) \(f.target)")
        case .missingAPI: return String(localized: "Asks \(f.count) times for \(f.resource), which the cluster does not serve (404)")
        case .missingObject: return String(localized: "Reads \(f.target) \(f.count) times, but it does not exist (404)")
        case .conflicts: return String(localized: "Its \(f.verb) of \(f.target) conflict \(f.count) times (409)")
        case .alreadyExists: return String(localized: "Creates \(f.resource) that already exist, \(f.count) times (409)")
        case .hotObject: return String(localized: "Rewrites \(f.target) every \(formatSeconds(f.value))")
        case .eventSpam: return String(localized: "Creates \(formatRate(f.rate)) events")
        case .slow: return String(localized: "\(f.verb) \(f.target) takes \(formatMs(f.value)) on average")
        case .serverErrors: return String(localized: "\(f.verb) \(f.target) fails \(f.count) times (\(f.code))")
        case .widespreadErrors: return String(localized: "\(f.actors) clients get server errors (\(f.code)), mostly on \(f.verb) \(f.resource)")
        case .widespreadSlow: return String(localized: "\(f.actors) clients wait \(formatMs(f.value)) on average")
        case .widespreadWatchChurn: return String(localized: "\(f.actors) clients' watches end every \(formatSeconds(f.value))")
        case .staleLog: return String(localized: "The API server on \(f.name) logged nothing for \(localizedDuration(Int64(f.value)))")
        case .unauthorized: return String(localized: "Refused \(f.count) times (401): its credentials are not valid")
        case nil: return kindName
        }
    }

    /// What to change, nil for a kind this version does not know.
    var fix: String? {
        switch kind {
        case .throttled: String(localized: "Lower its request rate (client QPS and burst), or give its flow schema a priority level of its own.")
        case .hotClient: String(localized: "Check its sync or poll interval, and whether it watches (informers) instead of polling.")
        case .listLoop: String(localized: "Polling with lists is expensive: a watch (informer) gets the same changes for a fraction of the load. Otherwise raise its poll interval.")
        case .watchChurn: String(localized: "Watches normally last several minutes. Look for a proxy or load balancer idle timeout between it and the API server, or the client restarting.")
        case .forbidden: String(localized: "Its RBAC lacks this permission: add it to its Role or ClusterRole, or stop the call.")
        case .missingAPI: String(localized: "The API was removed or its CRD is not installed: upgrade the client, or install the CRD.")
        case .missingObject: String(localized: "Something still points at a deleted object: check its configuration.")
        case .conflicts: String(localized: "Two writers update the same objects: look for a second controller, or an old replica still running.")
        case .alreadyExists: String(localized: "It should update the existing object instead: usually a controller bug or a version mismatch, which an upgrade fixes.")
        case .hotObject: String(localized: "A controller this busy on one object is usually fighting another writer, or flapping a status: compare its writes with the object owner’s.")
        case .eventSpam: String(localized: "Event floods load etcd: find the warning it repeats and fix its cause.")
        case .slow: String(localized: "Narrow the request (namespace, label selector), page large lists, or look at admission webhooks.")
        case .serverErrors: String(localized: "The API server could not answer: a failing admission webhook or aggregated API (metrics-server…) is the usual cause.")
        case .widespreadErrors: String(localized: "Not their fault: the API server or etcd is struggling. Check etcd health and latency, the API server logs and its nodes’ resources.")
        case .widespreadSlow: String(localized: "Not their fault: check etcd latency (its disk), the API server’s CPU and memory, and admission webhooks.")
        case .widespreadWatchChurn: String(localized: "Something between them and the API server cuts long connections: a load balancer or proxy idle timeout, or API servers restarting.")
        case .staleLog: String(localized: "It may be down, unreachable for clients, or unable to write its log: check its health and the node’s disk.")
        case .unauthorized: String(localized: "Its token or certificate expired or was deleted (a removed service account, an old kubeconfig): give it new credentials, or stop it. The log only knows its address.")
        case nil: nil
        }
    }
}
