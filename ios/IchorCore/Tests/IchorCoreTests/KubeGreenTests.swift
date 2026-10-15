import XCTest
@testable import IchorCore

final class KubeGreenTests: XCTestCase {
    func testDecodesSleepInfos() throws {
        let json = #"""
        {"kubeGreen":{"version":"v0.7.1","error":"",
         "sleepInfos":[
          {"namespace":"staging","name":"nights","weekdays":"1-5","sleepAt":"20:00","wakeUpAt":"07:00","timeZone":"Europe/Rome",
           "suspendDeployments":true,"suspendStatefulSets":true,"suspendCronJobs":false,
           "excludeCount":1,"includeCount":0,"patchCount":0,
           "operation":"SLEEP","lastScheduleAt":1000,"nextSleepAt":2000,"nextWakeUpAt":3000,"asleep":true,"health":"ok","reasons":[]},
          {"namespace":"sandbox","name":"until-changed","weekdays":"1-5","sleepAt":"19:00","wakeUpAt":"",
           "operation":"SLEEP","lastScheduleAt":1000,"nextSleepAt":2000,"nextWakeUpAt":0,"asleep":true,
           "health":"warning","reasons":["noWakeUp"]},
          {"namespace":"staging","name":"broken","weekdays":"1-5","sleepAt":"25:00","wakeUpAt":"07:00",
           "operation":"","lastScheduleAt":0,"nextSleepAt":0,"nextWakeUpAt":0,"asleep":false,
           "health":"warning","reasons":["invalidSchedule"]},
          {"namespace":"preview","name":"fresh","weekdays":"*","sleepAt":"22:30","wakeUpAt":"06:00",
           "operation":"","lastScheduleAt":0,"nextSleepAt":2000,"nextWakeUpAt":3000,"asleep":false,
           "health":"idle","reasons":["neverRan"]}]}}
        """#
        let services = try JSONDecoder().decode(DataServices.self, from: Data(json.utf8))
        let k = try XCTUnwrap(services.kubeGreen)
        XCTAssertEqual(k.version, "v0.7.1")
        XCTAssertEqual(k.sleepInfos.count, 4)

        let nights = try XCTUnwrap(k.sleepInfos.first)
        XCTAssertTrue(nights.asleep)
        XCTAssertEqual(nights.health, .ok)
        XCTAssertEqual(nights.excludeCount, 1)

        XCTAssertEqual(k.sleepInfos[1].reasons, [.noWakeUp])
        XCTAssertEqual(k.sleepInfos[1].nextWakeUpAt, 0)
        XCTAssertEqual(k.sleepInfos[2].reasons, [.invalidSchedule])
        XCTAssertEqual(k.sleepInfos[3].health, .idle)
        XCTAssertEqual(services.detected.last, .kubeGreen)
    }

    func testDefaultsAndSummary() throws {
        // Absent suspend flags are true, as upstream defaults; an unknown reason is dropped.
        let json = #"""
        {"kubeGreen":{"version":"","error":"",
         "sleepInfos":[
          {"namespace":"staging","name":"nights","sleepAt":"20:00","wakeUpAt":"07:00",
           "operation":"SLEEP","asleep":true,"health":"warning","reasons":["noWakeUp","somethingNew"]}]}}
        """#
        let services = try JSONDecoder().decode(DataServices.self, from: Data(json.utf8))
        let k = try XCTUnwrap(services.kubeGreen)
        let info = try XCTUnwrap(k.sleepInfos.first)
        XCTAssertTrue(info.suspendDeployments)
        XCTAssertTrue(info.suspendStatefulSets)
        XCTAssertFalse(info.suspendCronJobs)
        XCTAssertEqual(info.reasons, [.noWakeUp])
        XCTAssertEqual(k.summary, ServiceSummary(total: 1, attention: 1, health: .warning))
    }
}
