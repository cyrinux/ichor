import Foundation

// The Monitoring screen's texts (targets down, rules, operator): each is a key of
// Localizable.xcstrings and must match it exactly. Arguments are already formatted strings.

enum PromMonitoringText {
    static var entry: String { String(localized: "Targets, rules and operator") }
    static var openFromCheckup: String { String(localized: "Open the down targets and the rules") }
    static var targets: String { String(localized: "Targets") }
    static var rules: String { String(localized: "Rules") }
    static func targetsDown(_ a: String, _ b: String) -> String { String(localized: "Targets down: \(a) of \(b)") }
    static func notScrapedYet(_ a: String) -> String { String(localized: "Not scraped yet: \(a)") }
    static var targetsTruncated: String { String(localized: "Only the first 500 down targets are listed.") }
    static var allTargetsUp: String { String(localized: "Every target is up.") }
    static func poolDown(_ a: String, _ b: String) -> String { String(localized: "\(a) / \(b) down") }
    static func lastScrape(_ a: String) -> String { String(localized: "Last scrape \(a) ago") }
    static var neverScraped: String { String(localized: "Never scraped") }
    static func podLine(_ a: String) -> String { String(localized: "Pod \(a)") }
    static func serviceLine(_ a: String) -> String { String(localized: "Service \(a)") }
    static func ruleCounts(_ a: String, _ b: String) -> String { String(localized: "Groups: \(a)  ·  Rules: \(b)") }
    static func ruleStates(_ a: String, _ b: String, _ c: String) -> String {
        String(localized: "Firing: \(a)  ·  Pending: \(b)  ·  Errors: \(c)")
    }
    static var rulesTruncated: String { String(localized: "Only the first 3000 rules are listed; the counts cover them all.") }
    static var noRules: String { String(localized: "This Prometheus has no rules.") }
    static var errorsLabel: String { String(localized: "Errors") }
    static var inactive: String { String(localized: "Inactive") }
    static var alerting: String { String(localized: "Alerting") }
    static var recording: String { String(localized: "Recording") }
    static var healthErr: String { String(localized: "Evaluation error") }
    static func activeAlerts(_ a: String) -> String { String(localized: "Active alerts: \(a)") }
    static var noObjects: String { String(localized: "None in the cluster.") }
    static var monitors: String { String(localized: "Monitors and rules") }
    static var healthCritical: String { String(localized: "Unavailable") }
    static func replicasAvailable(_ a: String, _ b: String) -> String { String(localized: "\(a) of \(b) available") }
    static func shards(_ a: String) -> String { String(localized: "Shards: \(a)") }
    static var paused: String { String(localized: "Paused") }
    static var checkupSectionMonitoringHint: String { String(localized: "Scrape targets down and rules that fail to evaluate") }
    static func checkupScrapeTargetsDown(_ a: String, _ b: String) -> String { String(localized: "Scrape targets down: \(a) of \(b)") }
    static func checkupRuleErrors(_ a: String) -> String { String(localized: "Rules failing to evaluate: \(a)") }
    static func checkupGroupErrors(_ a: String, _ b: String) -> String { String(localized: "Rules failing to evaluate: \(a) (file \(b))") }
    static var checkupScrapeTargetsDownFix: String { String(localized: "Prometheus cannot read their metrics, so the alerts built on them stay silent. Usually a pod that is gone or restarting, a port or path that changed, or a NetworkPolicy blocking Prometheus: each target’s error says which.") }
    static var checkupRuleErrorsFix: String { String(localized: "A rule that fails to evaluate never fires: fix its expression in the PrometheusRule. A many-to-many match or a label that changed are the usual causes.") }
    static var checkupGroupErrorsFix: String { String(localized: "A rule that fails to evaluate never fires: fix its expression in the rule file named here (it comes from the Prometheus configuration, not a PrometheusRule).") }
}
