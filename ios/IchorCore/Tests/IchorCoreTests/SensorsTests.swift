import XCTest
@testable import IchorCore

final class SensorsTests: XCTestCase {
    func testDecodeGoJSON() throws {
        let json = #"""
        {"temperatures":[{"chip":"coretemp","label":"Package id 0","celsius":61.3,"critCelsius":100,"maxCelsius":85}],
        "fans":[{"chip":"nct6775","label":"CPU fan","rpm":1180}],"voltages":[{"chip":"nct6775","label":"Vcore","volts":0.912}],
        "thermalZones":[{"type":"x86_pkg_temp","celsius":61}],
        "cpuFreq":[{"cpu":0,"currentMhz":2000,"minMhz":800,"maxMhz":3400,"governor":"performance"}],
        "throttle":{"coreEvents":15,"packageEvents":5},"throttled":true,
        "pci":[{"id":"0000:00:1f.6","class":"Network controller","subclass":"Ethernet controller","vendor":"Intel Corporation","product":"I219-LM","driver":"e1000e"}],
        "errors":{}}
        """#
        let s = try TalosJSON.decode(NodeSensors.self, from: json)
        XCTAssertTrue(s.throttled)
        XCTAssertEqual(s.temperatures.first?.celsius, 61.3)
        XCTAssertEqual(s.throttle?.coreEvents, 15)
        XCTAssertEqual(s.pci.first?.class, "Network controller")
        XCTAssertEqual(s.cpuFreq.first?.governor, "performance")
        XCTAssertFalse(s.sensorsEmpty)
        XCTAssertNil(s.sensorsError)
    }

    func testVMAndNulls() throws {
        let json = #"{"temperatures":null,"fans":[],"voltages":null,"thermalZones":[],"cpuFreq":null,"throttle":null,"throttled":false,"pci":null,"errors":{"pci":"not available"}}"#
        let s = try TalosJSON.decode(NodeSensors.self, from: json)
        XCTAssertTrue(s.sensorsEmpty)
        XCTAssertNil(s.throttle)
        // A PCI failure belongs to the PCI section.
        XCTAssertNil(s.sensorsError)
        XCTAssertEqual(NodeSensors(errors: ["cpuFreq": "boom"]).sensorsError, "boom")
    }

    func testTemperatureLimits() {
        XCTAssertEqual(TemperatureSensor(celsius: 42.5, critCelsius: 100, maxCelsius: 85).limitFraction ?? -1, 0.5, accuracy: 0.001)
        XCTAssertEqual(TemperatureSensor(celsius: 50, critCelsius: 100).limitFraction ?? -1, 0.5, accuracy: 0.001)
        XCTAssertEqual(TemperatureSensor(celsius: 120, maxCelsius: 85).limitFraction ?? -1, 1, accuracy: 0.001)
        XCTAssertNil(TemperatureSensor(celsius: 40).limitFraction)
        XCTAssertTrue(TemperatureSensor(celsius: 86, maxCelsius: 85).hot)
        XCTAssertTrue(TemperatureSensor(celsius: 100, critCelsius: 100).hot)
        XCTAssertFalse(TemperatureSensor(celsius: 84, critCelsius: 100, maxCelsius: 85).hot)
    }
}
