import XCTest
@testable import IchorCore

final class CronJobsTests: XCTestCase {
    private let jobs = [
        KubeCronJob(namespace: "shop", name: "db-backup", title: "Database backup", schedule: "0 3 * * *", state: "succeeded", images: ["postgres:17"]),
        KubeCronJob(namespace: "shop", name: "mailer", description: "Weekly report", state: "failed"),
        KubeCronJob(namespace: "infra", name: "renew", state: "running"),
        KubeCronJob(namespace: "a", name: "never-ran", state: "never"),
    ]

    func testDecodesTheGoJSON() throws {
        let json = """
        {"cronJobs":[{"namespace":"shop","name":"db-backup","title":"Database backup","icon":"postgresql","remoteIcon":"",
        "schedule":"0 3 * * *","suspended":false,"triggerable":false,"active":0,"state":"failed","lastSchedule":1,
        "nextRun":2,"images":["postgres:17"],
        "runs":[{"name":"db-backup-manual-x","state":"failed","manual":true,"started":1000,"finished":95000}]}]}
        """
        let c = try XCTUnwrap(try TalosJSON.decode(KubeCronJobPage.self, from: json).cronJobs.first)
        XCTAssertEqual(c.id, "shop/db-backup")
        XCTAssertEqual(c.displayName, "Database backup")
        XCTAssertEqual(c.runState, .failed)
        XCTAssertFalse(c.triggerable)
        XCTAssertNil(c.remoteIcon)
        XCTAssertEqual(c.iconApp.iconSource(remoteIcons: false), .bundled("postgresql"))
        XCTAssertEqual(c.runs.first?.manual, true)
        XCTAssertEqual(c.runs.first?.duration, 94)
    }

    func testRunningAndFailedFirst() {
        XCTAssertEqual(filterCronJobs(jobs, namespace: nil, query: "").map(\.name), ["renew", "mailer", "never-ran", "db-backup"])
    }

    func testFiltersByNamespaceTitleDescriptionScheduleOrImage() {
        XCTAssertEqual(filterCronJobs(jobs, namespace: "shop", query: "").map(\.name), ["mailer", "db-backup"])
        XCTAssertEqual(filterCronJobs(jobs, namespace: nil, query: "DATABASE").map(\.name), ["db-backup"])
        XCTAssertEqual(filterCronJobs(jobs, namespace: nil, query: "weekly").map(\.name), ["mailer"])
        XCTAssertEqual(filterCronJobs(jobs, namespace: nil, query: "0 3").map(\.name), ["db-backup"])
        XCTAssertEqual(filterCronJobs(jobs, namespace: nil, query: "postgres").map(\.name), ["db-backup"])
        XCTAssertEqual(cronJobNamespaces(jobs), ["a", "infra", "shop"])
    }

    func testNoIconFallsBackAndRunningHasNoDuration() {
        let c = KubeCronJob(namespace: "a", name: "b")
        XCTAssertEqual(c.displayName, "b")
        XCTAssertEqual(c.iconApp.iconSource(remoteIcons: true), .monogram)
        XCTAssertNil(KubeJobRun(name: "r", started: 1000).duration)
    }
}
