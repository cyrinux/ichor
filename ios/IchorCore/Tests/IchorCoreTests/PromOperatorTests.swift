import XCTest
@testable import IchorCore

final class PromOperatorTests: XCTestCase {
    // PromRules (go/ichorgo/prom_rules.go), cut down.
    private let rulesJSON = #"""
    {"groups":[{"name":"hello-ichor.rules","file":"/etc/prometheus/rules/prometheus-x-rulefiles-0/demo-hello-ichor-1a2b.yaml",
      "ruleNamespace":"demo","ruleName":"hello-ichor","interval":30,"evaluationTime":0.0042,"lastEvaluation":1791600000000,
      "lastError":"many-to-many matching not allowed","firing":0,"pending":0,"errors":1,
      "rules":[{"name":"hello_ichor:http_requests:rate5m","type":"recording","state":"inactive","health":"err",
                "lastError":"many-to-many matching not allowed","query":"sum by (pod) (rate(x[5m]))","severity":"","duration":0,"alerts":0,
                "lastEvaluation":1791600000000}]},
      {"name":"node.rules","file":"/etc/prometheus/rules/custom.yaml","interval":60,"firing":1,"pending":0,"errors":0,
      "rules":[{"name":"NodeDown","type":"alerting","state":"firing","health":"ok","severity":"critical","duration":300,"alerts":1},
               {"name":"Future","type":"streaming","state":"sleeping","health":"maybe"}]}],
     "counts":{"groups":4,"rules":8,"firing":1,"pending":0,"errors":1},"truncated":false}
    """#

    private let targetsJSON = #"""
    {"pools":[{"pool":"serviceMonitor/demo/hello-ichor/0","kind":"ServiceMonitor","namespace":"demo","name":"hello-ichor","endpoint":0,
      "up":1,"down":2,"unknown":0,
      "targets":[{"scrapeUrl":"http://10.0.0.32:8080/metrics","lastError":"connection refused","lastScrape":1791600000000,
                  "lastScrapeDuration":0.012,"job":"hello-ichor","namespace":"demo","service":"hello-ichor","pod":"hello-ichor-abc","instance":"10.0.0.32:8080"},
                 {"scrapeUrl":"https://10.0.0.5:10250/metrics","lastError":"timeout","job":"kubelet","namespace":"kube-system","service":"kubelet","pod":"","instance":"10.0.0.5:10250"}]},
      {"pool":"podMonitor/apps/worker/1","kind":"PodMonitor","namespace":"apps","name":"worker","endpoint":1,"up":3,"down":0,"unknown":1},
      {"pool":"probe/web/site","kind":"Probe","namespace":"web","name":"site","endpoint":-1,"up":1},
      {"pool":"scrapeConfig/web/static","kind":"ScrapeConfig","namespace":"web","name":"static","endpoint":-1,"up":1},
      {"pool":"blackbox","kind":"","namespace":"","name":"","endpoint":-1,"up":2,"down":1,
       "targets":[{"scrapeUrl":"http://example.test/probe","lastError":"503","instance":"example.test"}]}],
     "up":13,"down":3,"unknown":1,"total":17,"truncated":false}
    """#

    private let operatorJSON = #"""
    {"installed":true,"error":"podmonitors is forbidden",
     "prometheuses":[{"namespace":"monitoring","name":"main","version":"v3.5.0","replicas":2,"shards":1,"desired":2,"available":1,"paused":false,
       "health":"warning","conditions":[{"type":"Available","status":"Degraded","reason":"SomePodsNotReady","message":"1/2"},{"type":"Reconciled","status":"True"}]}],
     "alertmanagers":[{"namespace":"monitoring","name":"main","replicas":1,"shards":1,"desired":1,"available":1,"health":"ok"}],
     "serviceMonitors":14,"podMonitors":2,"prometheusRules":35,"probes":0}
    """#

    func testRulesDecodeWithTheirStatesAndLinks() throws {
        let rules = try TalosJSON.decode(PromRules.self, from: rulesJSON)
        XCTAssertEqual(rules.counts.rules, 8)
        XCTAssertEqual(rules.counts.errors, 1)
        XCTAssertFalse(rules.truncated)
        let failing = rules.groups[0]
        XCTAssertTrue(failing.inTrouble)
        XCTAssertEqual(failing.rules[0].type, .recording)
        XCTAssertEqual(failing.rules[0].health, .err)
        XCTAssertEqual(failing.ruleLink, KubeObjectLink(
            resource: KubeAPIResource(group: "monitoring.coreos.com", resource: "prometheusrules", kind: "PrometheusRule"),
            namespace: "demo", name: "hello-ichor"))
        // A group from a file the operator did not write has no PrometheusRule to open.
        let custom = rules.groups[1]
        XCTAssertNil(custom.ruleLink)
        XCTAssertEqual(custom.rules[0].state, .firing)
        XCTAssertEqual(custom.rules[0].alerts, 1)
        XCTAssertEqual(custom.rules[0].duration, 300)
        // Unknown wire values fall back rather than failing the decode.
        XCTAssertEqual(custom.rules[1].type, .recording)
        XCTAssertEqual(custom.rules[1].state, .inactive)
        XCTAssertEqual(custom.rules[1].health, .unknown)
        XCTAssertEqual(try TalosJSON.decode(PromRules.self, from: "{}").counts, PromRuleCounts())
    }

    func testTargetsDecodeAndPoolsMapToTheirMonitor() throws {
        let targets = try TalosJSON.decode(PromTargets.self, from: targetsJSON)
        XCTAssertEqual(targets.down, 3)
        XCTAssertEqual(targets.total, 17)
        XCTAssertEqual(targets.troubledPools.map(\.pool), ["serviceMonitor/demo/hello-ichor/0", "blackbox"])
        let pools = targets.pools
        XCTAssertEqual(pools[0].label, "ServiceMonitor demo/hello-ichor")
        XCTAssertEqual(pools[0].monitorLink?.resource.resource, "servicemonitors")
        XCTAssertEqual(pools[0].monitorLink?.resource.group, "monitoring.coreos.com")
        XCTAssertEqual(pools[0].monitorLink?.resource.version, "v1")
        XCTAssertEqual(pools[1].monitorLink, KubeObjectLink(
            resource: KubeAPIResource(group: "monitoring.coreos.com", resource: "podmonitors", kind: "PodMonitor"), namespace: "apps", name: "worker"))
        XCTAssertEqual(pools[2].monitorLink?.resource.resource, "probes")
        XCTAssertEqual(pools[3].monitorLink?.resource.resource, "scrapeconfigs")
        XCTAssertEqual(pools[3].monitorLink?.resource.version, "v1alpha1")
        // A job of the Prometheus configuration has no object behind it.
        XCTAssertNil(pools[4].monitorLink)
        XCTAssertEqual(pools[4].label, "blackbox")
        XCTAssertEqual(pools[4].endpoint, -1)
    }

    func testADownTargetLinksToItsPodThenServiceThenMonitor() throws {
        let pools = try TalosJSON.decode(PromTargets.self, from: targetsJSON).pools
        let app = pools[0].targets[0]
        XCTAssertEqual(app.link(in: pools[0]), KubeObjectLink(resource: KubeAPIResource(resource: "pods", kind: "Pod"), namespace: "demo", name: "hello-ichor-abc"))
        XCTAssertEqual(app.serviceLink?.resource.resource, "services")
        // The kubelet has no pod: its Service.
        let kubelet = pools[0].targets[1]
        XCTAssertNil(kubelet.podLink)
        XCTAssertEqual(kubelet.link(in: pools[0]), KubeObjectLink(resource: KubeAPIResource(resource: "services", kind: "Service"), namespace: "kube-system", name: "kubelet"))
        // Neither pod nor Service nor monitor: nothing to open.
        XCTAssertNil(pools[4].targets[0].link(in: pools[4]))
        XCTAssertEqual(pools[4].targets[0].id, "http://example.test/probe")
    }

    func testOperatorStatusDecodesWithItsWorstHealth() throws {
        let status = try TalosJSON.decode(PromOperatorStatus.self, from: operatorJSON)
        XCTAssertTrue(status.installed)
        XCTAssertEqual(status.error, "podmonitors is forbidden")
        XCTAssertEqual(status.health, .warning)
        let prom = status.prometheuses[0]
        XCTAssertEqual(prom.desired, 2)
        XCTAssertEqual(prom.available, 1)
        XCTAssertFalse(prom.conditions[0].healthy)
        XCTAssertTrue(prom.conditions[1].healthy)
        XCTAssertEqual(status.serviceMonitors, 14)
        XCTAssertEqual(status.prometheusRules, 35)
        XCTAssertEqual(PromOperatorStatus.link(prometheus: prom).resource.resource, "prometheuses")
        XCTAssertEqual(PromOperatorStatus.link(alertmanager: status.alertmanagers[0]).resource.kind, "Alertmanager")
        let absent = try TalosJSON.decode(PromOperatorStatus.self, from: #"{"installed":false,"prometheuses":null}"#)
        XCTAssertFalse(absent.installed)
        XCTAssertEqual(absent.prometheuses, [])
        XCTAssertEqual(absent.health, .ok)
    }

    func testTheCheckupKnowsTheMonitoringSection() throws {
        let json = #"""
        {"status":"warning","sections":[{"id":"monitoring","status":"warning","checked":23,"findings":[
          {"kind":"scrapeTargetsDown","severity":"warning","namespace":"monitoring","name":"prometheus-operated","count":2,"limit":15,"value":13.3,
           "extra":"serviceMonitor/demo/hello-ichor/0"},
          {"kind":"prometheusRuleErrors","severity":"warning","namespace":"demo","name":"hello-ichor","count":1,
           "reason":"hello_ichor:http_requests:rate5m","message":"many-to-many matching not allowed"},
          {"kind":"ruleGroupErrors","severity":"warning","name":"custom.rules","extra":"/etc/prometheus/rules/custom.yaml","count":1,
           "reason":"Broken","message":"parse error"}]}]}
        """#
        let report = try TalosJSON.decode(CheckupReport.self, from: json)
        let section = report.sections[0]
        XCTAssertEqual(section.section, .monitoring)
        XCTAssertEqual(section.findings.compactMap(\.kind), [CheckupKind.scrapeTargetsDown, .prometheusRuleErrors, .ruleGroupErrors])
        XCTAssertEqual(section.findings[1].detail, "hello_ichor:http_requests:rate5m: many-to-many matching not allowed")
        XCTAssertEqual(section.findings[2].subject, "custom.rules")
        XCTAssertEqual(report.alertIssues["monitoring|prometheusRuleErrors|demo/hello-ichor"], "warning")
    }
}
