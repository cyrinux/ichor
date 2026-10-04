import XCTest
@testable import IchorCore

final class VeleroTests: XCTestCase {
    private let json = #"""
    {"velero":{"version":"v1","error":"",
     "schedules":[
      {"namespace":"velero","name":"failing","schedule":"0 3 * * *","paused":false,"phase":"Enabled","validationErrors":[],
       "health":"critical","reasons":["failed"],"storageLocation":"default","includedNamespaces":[],
       "lastBackup":{"name":"failing-1","phase":"Failed","startedAt":1000,"completedAt":2000,"errors":0,"warnings":0,"failureReason":"error getting backup store"},
       "lastSuccessAt":500,"inProgress":false},
      {"namespace":"velero","name":"partial","schedule":"@daily","phase":"Enabled","health":"warning","reasons":["partiallyFailed"],
       "storageLocation":"default","includedNamespaces":["app"],
       "lastBackup":{"name":"partial-1","phase":"PartiallyFailed","completedAt":2000,"errors":2,"warnings":3},"lastSuccessAt":1000,"inProgress":true},
      {"namespace":"velero","name":"invalid","schedule":"nope","phase":"FailedValidation","validationErrors":["invalid schedule"],
       "health":"warning","reasons":["invalid"],"lastBackup":null,"lastSuccessAt":0},
      {"namespace":"velero","name":"daily","schedule":"0 2 * * *","health":"ok","reasons":[],
       "lastBackup":{"name":"daily-1","phase":"Completed","completedAt":3000},"lastSuccessAt":3000},
      {"namespace":"velero","name":"paused","schedule":"0 2 * * *","paused":true,"health":"idle","reasons":[]}],
     "adhoc":[{"namespace":"velero","name":"before-upgrade","phase":"PartiallyFailed","completedAt":2500,"errors":1,"warnings":0,
       "failureReason":"","storageLocation":"default","health":"warning"}],
     "locations":[
      {"namespace":"velero","name":"offsite","provider":"aws","bucket":"offsite","default":false,"phase":"Unavailable","message":"rpc error","health":"critical"},
      {"namespace":"velero","name":"default","provider":"aws","bucket":"backups","default":true,"phase":"Available","health":"ok"}]}}
    """#

    private var services: DataServices { get throws { try TalosJSON.decode(DataServices.self, from: json) } }

    func testDecodesSchedulesBackupsAndLocations() throws {
        let s = try services
        let v = try XCTUnwrap(s.velero)
        XCTAssertEqual(v.schedules.count, 5)
        XCTAssertEqual(v.schedules[0].reasons, [.failed])
        XCTAssertEqual(v.schedules[0].lastBackup?.failureReason, "error getting backup store")
        XCTAssertEqual(v.schedules[1].lastBackup?.errors, 2)
        XCTAssertTrue(v.schedules[1].inProgress)
        XCTAssertNil(v.schedules[2].lastBackup)
        XCTAssertEqual(v.schedules[4].health, .idle)
        XCTAssertEqual(v.adhoc.map(\.label), ["velero/before-upgrade"])
        XCTAssertEqual(v.adhoc.first?.backup.errors, 1)
        XCTAssertEqual(v.locations.map(\.isDefault), [false, true])
        XCTAssertEqual(s.detected, [.velero])
    }

    func testSummaryCountsSchedulesAndLooksAtLocationsAndAdhoc() throws {
        // 3 schedules, the unavailable location and the failed backup taken by hand need a look.
        XCTAssertEqual(try services.summary(.velero), ServiceSummary(total: 5, attention: 5, health: .critical))
        XCTAssertEqual(try services.likelyCauses(downNodes: ["node-1"]), [])
    }

    func testAlertsCoverSchedulesAndLocationsNotAdhoc() throws {
        XCTAssertEqual(dataIssuesOf(try services), [
            "velero|velero/failing": dataCritical, "velero|velero/invalid": dataWarning,
            "velero|BackupStorageLocation/velero/offsite": dataCritical, "velero|velero/partial": dataWarning,
        ])
    }

    func testHintsIncludeVelero() throws {
        let inventory = try TalosJSON.decode(ClusterInventory.self, from: #"{"apps":[{"id":"velero","name":"Velero"},{"id":"longhorn","name":"Longhorn"}]}"#)
        XCTAssertEqual(dataServiceHints(inventory), "longhorn,velero")
    }
}
