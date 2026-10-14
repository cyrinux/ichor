import XCTest
@testable import IchorCore

final class SnapshotsTests: XCTestCase {
    func testDecodesSnapshotAndBackupAlerts() throws {
        let json = """
        {"snapshots":{"installed":true,"version":"v1","error":"","contentsError":"","classesError":"","claimsError":"",
        "snapshots":[{"namespace":"shop","name":"nightly","pvc":"orders","class":"default","content":"","error":"","ready":true,"createdAt":1000,"ageSeconds":1}],
        "contents":[],"classes":[{"name":"default","driver":"csi.example.io","deletionPolicy":"Delete","default":true}],
        "claims":[{"namespace":"shop","name":"orders","lastGoodAt":0,"ageSeconds":-1,"watched":true,"stale":true},
        {"namespace":"shop","name":"scratch","lastGoodAt":0,"ageSeconds":-1,"watched":false,"stale":true}]},
        "backupJobs":{"error":"","jobsError":"","jobs":[{"namespace":"shop","name":"backup","schedule":"0 1 * * *","health":"critical","suspended":false,"lastSuccess":0,"ageSeconds":-1,"failureStreak":3},
        {"namespace":"shop","name":"paused","schedule":"0 1 * * *","health":"idle","suspended":true,"lastSuccess":0,"ageSeconds":-1,"failureStreak":3}]}}
        """
        let services = try TalosJSON.decode(DataServices.self, from: json)
        XCTAssertEqual(services.snapshots?.snapshots.first?.className, "default")
        XCTAssertEqual(services.detected, [.snapshots, .backupJobs])
        XCTAssertEqual(dataIssuesOf(services), ["snapshots|shop/orders": dataWarning, "backupjobs|shop/backup": dataCritical])
        let failed = try TalosJSON.decode(DataServices.self, from: json.replacingOccurrences(of: "\"jobsError\":\"\"", with: "\"jobsError\":\"Forbidden\"").replacingOccurrences(of: "\"error\":\"\"", with: "\"error\":\"Forbidden\""))
        XCTAssertTrue(dataIssuesOf(failed).isEmpty)
    }
}
