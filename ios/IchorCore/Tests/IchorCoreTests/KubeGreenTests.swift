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

    func testDecodesNamespaceState() throws {
        // What kube-green has done to one namespace, read when a workload row is opened.
        let json = #"""
        {"installed":true,"asleep":true,
         "sleepInfos":[{"namespace":"staging","name":"nights","weekdays":"1-5","sleepAt":"20:00","wakeUpAt":"07:00",
          "operation":"SLEEP","nextSleepAt":2000,"nextWakeUpAt":3000,"asleep":true,"health":"ok","reasons":[]}],
         "sleepsDeployments":true,"sleepsStatefulSets":true,"sleepsCronJobs":false,
         "nextSleepAt":2000,"nextWakeUpAt":3000,"filtered":false}
        """#
        let k = try JSONDecoder().decode(KubeGreenNamespace.self, from: Data(json.utf8))
        XCTAssertTrue(k.installed)
        XCTAssertTrue(k.asleep)
        XCTAssertTrue(k.sleeps("Deployment"))
        XCTAssertTrue(k.sleeps("StatefulSet"))
        XCTAssertFalse(k.sleeps("CronJob"))
        XCTAssertEqual(k.asleepInfo?.label, "staging/nights")
        XCTAssertEqual(k.changeUndoneAt, 3000)
    }

    func testNamespaceWarnsBeforeAChangeItWouldUndo() throws {
        let nowMs: Int64 = 1_000_000
        let now = Date(epochMillis: nowMs)

        let asleep = try JSONDecoder().decode(KubeGreenNamespace.self, from: Data(#"{"installed":true,"asleep":true,"nextWakeUpAt":3000}"#.utf8))
        XCTAssertEqual(asleep.warnBeforeChange(now: now), .asleep)
        XCTAssertEqual(asleep.changeUndoneAt, 3000)

        // Sleeps within a day: a change made now is undone at the next sleep.
        let soon = try JSONDecoder().decode(KubeGreenNamespace.self, from: Data(#"{"installed":true,"nextSleepAt":4600000}"#.utf8))
        XCTAssertEqual(soon.warnBeforeChange(now: now), .sleepsSoon)
        XCTAssertEqual(soon.changeUndoneAt, 4600000)

        // Sleeps further out: nothing to warn about.
        let far = try JSONDecoder().decode(KubeGreenNamespace.self, from: Data(#"{"installed":true,"nextSleepAt":1000000000000}"#.utf8))
        XCTAssertNil(far.warnBeforeChange(now: now))

        // A cluster without kube-green, or a refused list: nothing to say.
        let unknown = try JSONDecoder().decode(KubeGreenNamespace.self, from: Data("{}".utf8))
        XCTAssertFalse(unknown.installed)
        XCTAssertNil(unknown.warnBeforeChange(now: now))
        XCTAssertEqual(unknown.changeUndoneAt, 0)
        XCTAssertNil(unknown.asleepInfo)
    }

    func testScheduleEditorReadsAndWritesWeekdays() {
        XCTAssertEqual(KubeGreenSchedule.days("*"), Set(0...6))
        XCTAssertEqual(KubeGreenSchedule.days("1-5"), Set(1...5))
        XCTAssertEqual(KubeGreenSchedule.days("5-7"), [0, 5, 6])
        XCTAssertNil(KubeGreenSchedule.days("*/2"))
        XCTAssertNil(KubeGreenSchedule.days("MON"))
        XCTAssertEqual(KubeGreenSchedule.weekdays(Set(0...6)), "*")
        XCTAssertEqual(KubeGreenSchedule.weekdays(Set(1...5)), "1-5")
        XCTAssertEqual(KubeGreenSchedule.weekdays([6, 0]), "0,6")
        XCTAssertEqual(KubeGreenSchedule.weekdays([1, 3, 4]), "1,3-4")
    }

    func testScheduleEditorRefusesWhatTheCoreRefuses() {
        XCTAssertTrue(KubeGreenSchedule.timeValid("20:00"))
        XCTAssertTrue(KubeGreenSchedule.timeValid("*/2:30"))
        XCTAssertFalse(KubeGreenSchedule.timeValid("25:00"))
        XCTAssertFalse(KubeGreenSchedule.timeValid("20:61"))
        XCTAssertFalse(KubeGreenSchedule.timeValid("20h"))
        XCTAssertTrue(KubeGreenSchedule.weekdaysValid("1-5"))
        XCTAssertFalse(KubeGreenSchedule.weekdaysValid("mon-fri"))
        XCTAssertTrue(KubeGreenSchedule.zoneValid("Europe/Paris"))
        XCTAssertFalse(KubeGreenSchedule.zoneValid("Mars/Olympus"))
    }
}
