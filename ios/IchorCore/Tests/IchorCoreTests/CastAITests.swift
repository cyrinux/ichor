import XCTest
@testable import IchorCore

/// Mirrors the Android CastAITest: the same wire JSON, the same expectations.
final class CastAITests: XCTestCase {
    private let json = #"""
    {"castai":{"version":"v1","error":"","compared":2,"cpuDeltaMilli":-430,"memoryDeltaBytes":-268435456,
     "recommendations":[
      {"namespace":"shop","name":"worker-statefulset","kind":"StatefulSet","workload":"worker","mode":"immediate",
       "health":"critical","reasons":["vpa"],"message":"webhook unreachable",
       "containers":[{"name":"worker","cpu":"2","memory":"3Gi"}]},
      {"namespace":"shop","name":"report-cronjob","kind":"CronJob","workload":"report","mode":"deferred","readOnly":true,
       "health":"warning","reasons":["readOnly","later"],"message":"managed by another autoscaler",
       "containers":[{"name":"report","cpu":"50m","memory":"256Mi","originalCpu":"100m","originalMemory":"128Mi"}],
       "cpuDeltaMilli":-50,"memoryDeltaBytes":134217728},
      {"namespace":"shop","name":"api-deployment","kind":"Deployment","workload":"api","mode":"deferred","health":"ok","reasons":[],
       "containers":[{"name":"api","cpu":"120m","memory":"640Mi","cpuLimit":"1","memoryLimit":"1Gi","originalCpu":"500m","originalMemory":"1Gi"}],
       "cpuDeltaMilli":-380,"memoryDeltaBytes":-402653184}]}}
    """#

    private var services: DataServices { get throws { try TalosJSON.decode(DataServices.self, from: json) } }
    private var castai: CastAIStatus { get throws { try XCTUnwrap(try services.castai) } }

    func testDecodesRecommendations() throws {
        let c = try castai
        XCTAssertEqual(c.recommendations.count, 3)
        XCTAssertEqual(c.cpuDeltaMilli, -430)
        let api = try XCTUnwrap(c.recommendations.last)
        XCTAssertEqual(api.label, "shop/api")
        XCTAssertEqual(api.mode, .deferred)
        XCTAssertEqual(api.containers.first?.originalCpu, "500m")
        // An unknown reason from a newer core is dropped, the known one kept.
        XCTAssertEqual(c.recommendations[1].reasons, [.readOnly])
        XCTAssertEqual(c.recommendations[0].mode, .immediate)
    }

    func testSummaryCountsProblems() throws {
        let s = try services
        XCTAssertEqual(s.summary(.castai), ServiceSummary(total: 3, attention: 2, health: .critical))
        XCTAssertTrue(s.detected.contains(.castai))
    }

    func testOnlyUnappliedRecommendationsAlert() throws {
        let issues = dataIssuesOf(try services)
        XCTAssertEqual(issues["castai|shop/worker"], dataCritical)
        XCTAssertNil(issues["castai|shop/report"])
        XCTAssertNil(issues["castai|shop/api"])
        XCTAssertEqual(dataSystemTitle("castai|shop/worker"), "CAST AI")
    }

    func testFormatsMillicores() {
        XCTAssertEqual(formatMilliCores(-380, signed: true), "-380m")
        XCTAssertEqual(formatMilliCores(2000, signed: true), "+2")
        XCTAssertEqual(formatMilliCores(250), "250m")
        XCTAssertEqual(formatMilliCores(0, signed: true), "0")
    }

    func testFormatsSignedBytesAndChanges() {
        XCTAssertEqual(signedBytes(-402_653_184), "-384.0 MiB")
        XCTAssertEqual(signedBytes(134_217_728), "+128.0 MiB")
        XCTAssertEqual(signedBytes(0), "0 B")
        XCTAssertEqual(castAIChangeText("500m", "120m"), "500m → 120m")
        XCTAssertEqual(castAIChangeText("1", "1"), "1")
        XCTAssertEqual(castAIApprox("$120", estimated: true), "≈ $120")
        XCTAssertEqual(castAIApprox("$120", estimated: false), "$120")
        // Without a currency, the bare number.
        XCTAssertEqual(formatMoney(418.4, currency: ""), "418")
        XCTAssertEqual(formatMoney(0.1608, currency: "", decimals: 3), "0.161")
    }

    func testClassifiesChanges() throws {
        let c = try castai
        let (worker, report, api) = (c.recommendations[0], c.recommendations[1], c.recommendations[2])
        XCTAssertEqual(worker.change, .unknown)
        // Less CPU but more memory: it grows.
        XCTAssertEqual(report.change, .grow)
        XCTAssertEqual(api.change, .shrink)
        XCTAssertEqual(c.changeCounts, CastAIChangeCounts(shrink: 1, grow: 1, same: 0, unknown: 1))
        XCTAssertEqual(c.commonMode?.mode, .deferred)
        XCTAssertEqual(c.commonMode?.count, 2)
        XCTAssertNil(CastAIStatus().commonMode)
    }

    func testOverviewPutsProblemsThenGrowthThenSavings() throws {
        let c = try castai
        let sections = c.sections(.overview)
        XCTAssertEqual(sections.map(\.kind), [.attention, .grows, .reductions, .other])
        XCTAssertEqual(sections[0].rows.map(\.label), ["shop/report", "shop/worker"])
        XCTAssertEqual(sections[1].rows.map(\.label), ["shop/report"])
        XCTAssertEqual(sections[2].rows.map(\.label), ["shop/api"])
        // The filter narrows every section; one left empty is dropped.
        XCTAssertEqual(c.sections(.overview, query: "API").map(\.kind), [.reductions])
    }

    func testOverviewShowsTheFirstGrowersOnly() {
        let recs = (1...8).map { i in
            CastAIRecommendation(namespace: "ns", name: "w\(i)", workload: "w\(i)",
                                 containers: [CastAIContainer(name: "c", cpu: "1", originalCpu: "500m")],
                                 cpuDeltaMilli: Int64(100 * i))
        }
        let status = CastAIStatus(recommendations: recs)
        let grows = status.sections(.overview).first { $0.kind == .grows }
        XCTAssertEqual(grows?.rows.count, castAIGrowsPreview)
        XCTAssertEqual(grows?.hidden, 3)
        XCTAssertEqual(grows?.total, 8)
        // Biggest first.
        XCTAssertEqual(grows?.rows.first?.label, "ns/w8")
        XCTAssertEqual(status.sections(.grows).first?.rows.count, 8)
    }

    func testNamespacesCarryTheirTotals() throws {
        let c = try castai
        let two = c.with(recommendations: c.recommendations + [CastAIRecommendation(namespace: "ads", name: "x", workload: "x", cpuDeltaMilli: -10)])
        let sections = two.sections(.namespaces)
        XCTAssertEqual(sections.map(\.namespace), ["ads", "shop"])
        XCTAssertEqual(sections[1].cpuDeltaMilli, -430)
    }

    func testFlagsARequestNearItsMemoryLimit() {
        let near = CastAIRecommendation(containers: [CastAIContainer(memoryLimitPercent: 40), CastAIContainer(memoryLimitPercent: 88)])
        XCTAssertTrue(near.nearMemoryLimit)
        XCTAssertEqual(near.memoryLimitPercent, 88)
        XCTAssertFalse(CastAIRecommendation(containers: [CastAIContainer(memoryLimitPercent: 60)]).nearMemoryLimit)
    }

    func testAbsentSectionIsNotDetected() throws {
        let none = try TalosJSON.decode(DataServices.self, from: "{}")
        XCTAssertNil(none.castai)
        XCTAssertFalse(none.detected.contains(.castai))
    }

    func testHintsIncludeCastAI() throws {
        let inventory = try TalosJSON.decode(ClusterInventory.self, from: #"{"apps":[{"id":"castai","name":"CAST AI"},{"id":"rook","name":"Rook Ceph"}]}"#)
        XCTAssertEqual(dataServiceHints(inventory), "rook,castai")
    }
}
