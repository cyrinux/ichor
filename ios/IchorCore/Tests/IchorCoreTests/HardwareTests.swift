import XCTest
@testable import IchorCore

final class HardwareTests: XCTestCase {
    func testDecodeGoJSON() throws {
        let json = #"""
        {"system":{"manufacturer":"Intel","product":"NUC","version":"1","serial":"S1","uuid":"u","sku":"","biosVersion":"2.0"},
        "processors":[{"socket":"CPU 0","manufacturer":"Intel","model":"i5","cores":4,"threads":8,"maxSpeedMhz":4200,"bootSpeedMhz":1600}],
        "memory":[{"slot":"DIMM 0","bank":"A","sizeMib":16384,"type":"","speed":3200,"manufacturer":"Kingston","serial":"x"},
        {"slot":"DIMM 1","bank":"B","sizeMib":16384,"type":"","speed":3200,"manufacturer":"Kingston","serial":"y"}],
        "disks":[{"name":"sdb","devPath":"/dev/sdb","model":"","serial":"","size":1000,"type":"hdd","wwid":"","busPath":"","systemDisk":false,"readonly":false},
        {"name":"nvme0n1","devPath":"/dev/nvme0n1","model":"Samsung","serial":"","size":512110190592,"type":"nvme","wwid":"","busPath":"","systemDisk":true,"readonly":false}],
        "extensions":[{"name":"intel-ucode","version":"2024","author":"Sidero","description":"microcode"}],
        "security":{"secureBoot":true,"bootedWithUki":true,"ukiSigningKeyFingerprint":"","pcrSigningKeyFingerprint":"","selinuxState":"permissive","fipsState":"disabled","moduleSignatureEnforced":false},
        "errors":{}}
        """#
        let hw = try TalosJSON.decode(NodeHardware.self, from: json)
        XCTAssertEqual(hw.system?.product, "NUC")
        XCTAssertEqual(hw.totalCores, 4)
        XCTAssertEqual(hw.totalThreads, 8)
        XCTAssertEqual(hw.totalMemoryBytes, 32 * 1_073_741_824)
        XCTAssertEqual(sortedDisks(hw.disks).map(\.name), ["nvme0n1", "sdb"])
        XCTAssertEqual(hw.security?.secureBoot, true)
        XCTAssertEqual(hw.extensions.first?.name, "intel-ucode")
    }

    func testNullSectionsAndErrors() throws {
        let json = #"{"system":null,"processors":null,"memory":null,"disks":null,"extensions":null,"security":null,"errors":{"system":"not found","security":"unsupported"}}"#
        let hw = try TalosJSON.decode(NodeHardware.self, from: json)
        XCTAssertNil(hw.system)
        XCTAssertNil(hw.security)
        XCTAssertEqual(hw.disks, [])
        XCTAssertEqual(hw.totalMemoryBytes, 0)
        XCTAssertEqual(hw.errors["security"], "unsupported")
    }
}
