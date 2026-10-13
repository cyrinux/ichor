import Foundation

// Generated with Android's capacity_* resources from one table: each text is a key of
// Localizable.xcstrings and must match it exactly. Arguments are already formatted strings,
// in the order the English sentence names them.

enum CapacityText {
    static var capacityTitle: String { String(localized: "Capacity") }
    static var capacityIntro: String { String(localized: "Free room counts requests against allocatable, as the scheduler does; usage comes from metrics-server.") }
    static func capacityFreeCpu(_ a: String, _ b: String) -> String { String(localized: "\(a) CPU free of \(b)") }
    static func capacityFreeMemory(_ a: String, _ b: String) -> String { String(localized: "\(a) memory free of \(b)") }
    static func capacityFreePods(_ a: String, _ b: String) -> String { String(localized: "\(a) pod slots free of \(b)") }
    static var capacityNPlusOneOk: String { String(localized: "N+1: losing any one node leaves no pod Pending") }
    static func capacityNPlusOneShort(_ a: String, _ b: String) -> String { String(localized: "Losing \(a) leaves \(b) pods Pending") }
    static var capacityMetricsUnavailable: String { String(localized: "Usage unavailable: metrics-server is not installed. The bars show requests only.") }
    static var capacityMetricsForbidden: String { String(localized: "Usage unavailable: these credentials may not read metrics-server.") }
    static var capacityClassesUnread: String { String(localized: "PriorityClasses could not be read: priorities come from the pods.") }
    static var capacityWhatIf: String { String(localized: "What if a node goes down") }
    static var capacityNodesTitle: String { String(localized: "Nodes") }
    static var capacityWorkloadsTitle: String { String(localized: "Workloads, the first to go first") }
    static var capacitySectionSummary: String { String(localized: "Summary") }
    static var capacitySectionWorkloads: String { String(localized: "Workloads") }
    static func sectionRailJumpTo(_ a: String) -> String { String(localized: "Jump to \(a)") }
    static var capacityWorkloadsEmpty: String { String(localized: "No workload matches this filter.") }
    static var capacityFilterAll: String { String(localized: "All") }
    static var capacityFilterAtRisk: String { String(localized: "At risk") }
    static var capacityNoClass: String { String(localized: "No priority class") }
    static func capacityNodeBars(_ a: String, _ b: String, _ c: String) -> String { String(localized: "\(a), memory requests \(b) percent, usage \(c) percent") }
    static func capacityNodeBarsNoUsage(_ a: String, _ b: String) -> String { String(localized: "\(a), memory requests \(b) percent") }
    static var capacityNodeOpen: String { String(localized: "Show what losing this node costs") }
    static var capacityOvercommitted: String { String(localized: "memory limits exceed the node") }
    static var capacityRequests: String { String(localized: "Requests") }
    static var capacityUsage: String { String(localized: "Usage") }
    static func capacityNodeFree(_ a: String, _ b: String, _ c: String) -> String { String(localized: "Free: \(a) CPU, \(b) memory, \(c) pod slots") }
    static func capacityWorkloadLine(_ a: String, _ b: String, _ c: String) -> String { String(localized: "\(a) replicas, each asking \(b) CPU and \(c)") }
    static func capacityPriority(_ a: String) -> String { String(localized: "priority \(a)") }
    static func capacityMoreFit(_ a: String) -> String { String(localized: "\(a) more fit") }
    static func capacityRiskOomKills(_ a: String) -> String { String(localized: "OOM-killed \(a) times") }
    static var capacityRiskOomNear: String { String(localized: "near its memory limit") }
    static var capacityRiskEvictFirst: String { String(localized: "evicted first under node pressure (BestEffort)") }
    static var capacityRiskUnbounded: String { String(localized: "no memory limit (Burstable)") }
    static var capacityRiskOk: String { String(localized: "bounded") }
    static var capacityLossLoading: String { String(localized: "Simulating the loss of a node…") }
    static func capacityLossTitle(_ a: String) -> String { String(localized: "If \(a) goes down") }
    static func capacityLossWorst(_ a: String) -> String { String(localized: "\(a) is the node whose loss costs the most. Another one:") }
    static func capacityLossCandidate(_ a: String, _ b: String) -> String { String(localized: "\(a) (\(b) Pending)") }
    static func capacityLossLost(_ a: String, _ b: String, _ c: String, _ d: String) -> String { String(localized: "\(a) pods must move, asking \(b) CPU and \(c); \(d) DaemonSet or static pods go with the node.") }
    static func capacityLossPlaced(_ a: String) -> String { String(localized: "\(a) pods find room elsewhere") }
    static var capacityLossPreemptedTitle: String { String(localized: "Evicted to make room") }
    static func capacityLossPreemptedRow(_ a: String, _ b: String) -> String { String(localized: "\(a), evicted by \(b)") }
    static var capacityLossPdbViolated: String { String(localized: "breaks its disruption budget") }
    static var capacityLossAllFit: String { String(localized: "Every pod finds room elsewhere.") }
    static var capacityLossPendingTitle: String { String(localized: "Stay Pending") }
    static var capacityLossPendingCpu: String { String(localized: "not enough CPU") }
    static var capacityLossPendingMemory: String { String(localized: "not enough memory") }
    static var capacityLossPendingPods: String { String(localized: "no pod slot left") }
    static var capacityLossSuggestionsTitle: String { String(localized: "To make room, scale down") }
    static func capacityLossSuggestion(_ a: String, _ b: String, _ c: String) -> String { String(localized: "Scale \(a) from \(b) to \(c)") }
    static func capacityLossSuggestionDetail(_ a: String, _ b: String, _ c: String, _ d: String) -> String { String(localized: "\(a), priority \(b); frees \(c) CPU and \(d)") }
    static var capacityLossPdbBlocks: String { String(localized: "Its disruption budget lets no replica go.") }
    static func capacityLossPdbMin(_ a: String) -> String { String(localized: "Its disruption budget keeps \(a).") }
    static var capacityLossSystemNote: String { String(localized: "System classes (system-cluster-critical, system-node-critical) are never suggested.") }
    static var capacityLossEstimate: String { String(localized: "An estimate") }
    static var capacityLossOpenScale: String { String(localized: "Open the scale dialog") }
    static var capacityCheckupLink: String { String(localized: "Open the Capacity screen") }
    static var capacityLossNotEnough: String { String(localized: "Even then, not every Pending pod finds room.") }
    static var capacityLossNoScale: String { String(localized: "Scale it from its own screen.") }
}
