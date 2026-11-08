import XCTest
@testable import IchorCore

final class DisksTests: XCTestCase {
    private let gib: UInt64 = 1 << 30
    private let mib: UInt64 = 1 << 20

    func testDecodeGoJSON() throws {
        let json = #"""
            {"supported":true,"reason":"","disks":[{"name":"nvme0n1","devPath":"/dev/nvme0n1","size":512110190592,"type":"nvme",
            "transport":"nvme","model":"Sample NVMe 512","serial":"SN-0001","systemDisk":true,"readonly":false,"unallocated":1048576,
            "partitions":[
              {"index":1,"devPath":"/dev/nvme0n1p1","label":"EFI","volumeId":"EFI","offset":1048576,"size":104857600,"filesystem":"vfat",
               "encryption":"disabled","encryptionProvider":"","uuid":"0A1B-2C3D","partitionUuid":"11111111-2222-3333-4444-555555555555",
               "partitionType":"c12a7328-f81f-11d2-ba4b-00a0c93ec93b","mountedOn":"","phase":"ready","error":""},
              {"index":3,"devPath":"/dev/nvme0n1p3","label":"STATE","volumeId":"STATE","offset":105906176,"size":104857600,
               "filesystem":"xfs","encryption":"enabled","encryptionProvider":"luks2","phase":"ready"}],
            "segments":[{"kind":"partition","index":1,"label":"EFI","offset":1048576,"size":104857600},
                        {"kind":"partition","index":3,"label":"STATE","offset":105906176,"size":104857600},
                        {"kind":"free","offset":210763776,"size":1048576}]}]}
            """#
        let list = try TalosJSON.decode(NodeDisks.self, from: json)
        XCTAssertTrue(list.supported)
        let disk = try XCTUnwrap(list.disks.first)
        XCTAssertEqual(disk.transport, "nvme")
        XCTAssertTrue(disk.systemDisk)
        XCTAssertEqual(disk.unallocated, mib)
        XCTAssertEqual(disk.partitions.map(\.index), [1, 3])
        // A partition missing uuid and the other optional fields decodes with "".
        XCTAssertEqual(disk.partitions[1].uuid, "")
        XCTAssertEqual(disk.partitions[1].encryptionState, .enabled)
        XCTAssertEqual(disk.partitions[1].encryptionProvider, "luks2")
        XCTAssertTrue(disk.segments[2].isFree)
        XCTAssertEqual(disk.segments[2].index, 0)
    }

    func testDecodeUnsupportedAndNulls() throws {
        let json = #"{"supported":false,"reason":"disks need Talos v1.8 or newer","disks":null}"#
        let list = try TalosJSON.decode(NodeDisks.self, from: json)
        XCTAssertFalse(list.supported)
        XCTAssertEqual(list.disks, [])
        let bare = try TalosJSON.decode(DiskLayout.self, from: #"{"name":"sda","partitions":null,"segments":null}"#)
        XCTAssertEqual(bare.partitions, [])
        XCTAssertEqual(barSlices(bare), [])
    }

    func testFractionsSumToOneAndTinySliceWidened() {
        let disk = DiskLayout(
            name: "sda", size: 101 * gib,
            segments: [
                DiskSegment(kind: "partition", index: 1, label: "META", size: mib),
                DiskSegment(kind: "partition", index: 2, label: "EPHEMERAL", size: 100 * gib),
                DiskSegment(kind: "free", size: gib),
            ])
        let slices = barSlices(disk)
        XCTAssertEqual(slices.count, 3)
        XCTAssertEqual(slices.reduce(0) { $0 + $1.fraction }, 1, accuracy: 1e-9)
        XCTAssertEqual(slices[0].fraction, 0.015, accuracy: 1e-9)
        XCTAssertGreaterThan(slices[1].fraction, slices[2].fraction)
        XCTAssertEqual(slices.map(\.segment.label), ["META", "EPHEMERAL", ""])
    }

    func testEmptyAndZeroSizedSegmentsDrawNothing() {
        XCTAssertEqual(barSlices(DiskLayout(name: "sda", size: 0)), [])
        let zero = DiskLayout(name: "sda", segments: [DiskSegment(kind: "partition", index: 1, size: 0)])
        XCTAssertEqual(barSlices(zero), [])
        // Many tiny slices: the floor drops to 1/n so they still sum to 1.
        let tiny = DiskLayout(name: "sda", segments: (1...4).map { DiskSegment(kind: "partition", index: $0, size: 1) })
        XCTAssertEqual(barSlices(tiny, minFraction: 0.5).reduce(0) { $0 + $1.fraction }, 1, accuracy: 1e-9)
    }

    func testKnownLabelFallback() {
        XCTAssertEqual(DiskPartition(index: 1, label: "EFI", volumeId: "X").knownLabel, "EFI")
        XCTAssertEqual(DiskPartition(index: 2, label: " ", volumeId: "u-data").knownLabel, "u-data")
        XCTAssertNil(DiskPartition(index: 3).knownLabel)
    }

    func testEncryptionStateAndPhase() {
        XCTAssertEqual(DiskPartition(index: 1, encryption: "enabled").encryptionState, .enabled)
        XCTAssertEqual(DiskPartition(index: 1, encryption: "disabled").encryptionState, .disabled)
        XCTAssertEqual(DiskPartition(index: 1, encryption: "").encryptionState, .unknown)
        XCTAssertEqual(DiskPartition(index: 1, encryption: "something-new").encryptionState, .unknown)
        XCTAssertFalse(DiskPartition(index: 1, phase: "ready").phaseNeedsMention)
        XCTAssertTrue(DiskPartition(index: 1, phase: "waiting").phaseNeedsMention)
    }

    func testDiskInfoTransportIsOptional() throws {
        let old = try TalosJSON.decode(DiskInfo.self, from: #"{"name":"sdb","size":1000,"type":"hdd"}"#)
        XCTAssertEqual(old.transport, "")
        XCTAssertEqual(old.devPath, "")
        let new = try TalosJSON.decode(DiskInfo.self, from: #"{"name":"sdb","transport":"sata","systemDisk":true}"#)
        XCTAssertEqual(new.transport, "sata")
        XCTAssertTrue(new.systemDisk)
    }
}
