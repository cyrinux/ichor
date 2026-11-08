import Foundation

// Generated with Android's kb_tab_summary and kb_summary_* resources from one table: each text
// is a key of Localizable.xcstrings and must match it exactly.

enum SummaryText {
    static var tabSummary: String { String(localized: "Summary") }
    static var conditions: String { String(localized: "Conditions") }
    static var noConditions: String { String(localized: "This kind reports no conditions.") }
    static var owners: String { String(localized: "Owned and managed by") }
    static var controller: String { String(localized: "controller") }
    static var helmRelease: String { String(localized: "Helm release") }
    static var metadata: String { String(localized: "Metadata") }
    static var labels: String { String(localized: "Labels") }
    static var annotations: String { String(localized: "Annotations") }
    static var finalizers: String { String(localized: "Finalizers") }
    static func created(_ a: String) -> String { String(localized: "Created \(a) ago") }
    static func deleting(_ a: String) -> String { String(localized: "Deletion requested \(a) ago") }
    static func eventsError(_ a: String) -> String { String(localized: "Events could not be read: \(a)") }
    static var healthOK: String { String(localized: "Healthy") }
    static var healthWarn: String { String(localized: "Needs a look") }
    static var healthBad: String { String(localized: "Unhealthy") }
    static var healthNone: String { String(localized: "No health reported") }
    static var phase: String { String(localized: "Phase") }
    static var replicas: String { String(localized: "Replicas") }
    static var selector: String { String(localized: "Selector") }
    static var image: String { String(localized: "Image") }
    static var node: String { String(localized: "Node") }
    static var suspended: String { String(localized: "Suspended") }
    static var schedule: String { String(localized: "Schedule") }
}
