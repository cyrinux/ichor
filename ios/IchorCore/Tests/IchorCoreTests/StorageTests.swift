import XCTest
@testable import IchorCore

final class StorageTests: XCTestCase {
    func testMountsDecoding() throws {
        let decoded = try TalosJSON.decode(NodeMounts.self, from: """
        {"mounts":[{"filesystem":"/dev/sda4","mountedOn":"/var","size":1000,"available":150,"used":850,"usedPercent":85},
                   {"filesystem":"tmpfs","mountedOn":"/run","size":400,"available":300}]}
        """)
        XCTAssertEqual(decoded.mounts[0], MountInfo(filesystem: "/dev/sda4", mountedOn: "/var", size: 1000, available: 150, used: 850, usedPercent: 85))
        // Without used/usedPercent (an older core) they are derived.
        XCTAssertEqual(decoded.mounts[1].used, 100)
        XCTAssertEqual(decoded.mounts[1].usedPercent, 25)
        XCTAssertEqual(try TalosJSON.decode(NodeMounts.self, from: #"{"mounts":null}"#).mounts, [])
    }

    func testUsageThresholds() {
        XCTAssertEqual(usageLevel(percent: 0), .normal)
        XCTAssertEqual(usageLevel(percent: 79.9), .normal)
        XCTAssertEqual(usageLevel(percent: 80), .warning)
        XCTAssertEqual(usageLevel(percent: 89.9), .warning)
        XCTAssertEqual(usageLevel(percent: 90), .critical)
        XCTAssertEqual(usageLevel(percent: 100), .critical)
        XCTAssertEqual(usageFraction(percent: 85), 0.85)
        XCTAssertEqual(usageFraction(percent: 140), 1)
        XCTAssertEqual(usageFraction(percent: -3), 0)
        XCTAssertEqual(usageFraction(percent: .nan), 0)
    }

    func testMountSorting() {
        let mounts = [MountInfo(filesystem: "a", mountedOn: "/b", size: 9, usedPercent: 10), MountInfo(filesystem: "a", mountedOn: "/a", size: 9, usedPercent: 10),
                      MountInfo(filesystem: "a", mountedOn: "/var", size: 9, usedPercent: 92)]
        XCTAssertEqual(sortMounts(mounts).map(\.mountedOn), ["/var", "/a", "/b"])
    }

    func testListedMounts() {
        let mounts = [MountInfo(filesystem: "/dev/sda4", mountedOn: "/var", size: 100, usedPercent: 40),
                      MountInfo(filesystem: "/dev/sda3", mountedOn: "/system/state", size: 10, usedPercent: 70),
                      MountInfo(filesystem: "/dev/sda4", mountedOn: "/var/lib/kubelet/pods/x/volumes/y", size: 100, usedPercent: 40),
                      MountInfo(filesystem: "tmpfs", mountedOn: "/run", size: 50, usedPercent: 1),
                      MountInfo(filesystem: "/dev/loop0", mountedOn: "/", size: 0),
                      MountInfo(filesystem: "proc", mountedOn: "/proc")]
        // The node's own disk-backed filesystems, fullest first.
        XCTAssertEqual(listedMounts(mounts, showAll: false).map(\.mountedOn), ["/system/state", "/var"])
        XCTAssertEqual(listedMounts(mounts, showAll: true).count, 6)
        XCTAssertEqual(listedMounts(mounts, showAll: true).first?.mountedOn, "/system/state")
    }

    func testVolumesDecoding() throws {
        let decoded = try TalosJSON.decode(NodeVolumes.self, from: """
        {"supported":true,"volumes":[{"id":"EPHEMERAL","phase":"ready","type":"partition","location":"/dev/nvme0n1p4",
          "size":1024,"filesystem":"xfs","encryption":"luks2","mountedOn":"/var"},{"id":"STATE","phase":"failed","error":"no disk matched"}]}
        """)
        XCTAssertTrue(decoded.supported)
        XCTAssertEqual(decoded.volumes[0], VolumeInfo(id: "EPHEMERAL", phase: "ready", type: "partition", location: "/dev/nvme0n1p4",
                                                      size: 1024, filesystem: "xfs", encryption: "luks2", mountedOn: "/var"))
        XCTAssertTrue(decoded.volumes[0].isReady)
        XCTAssertFalse(decoded.volumes[1].isReady)
        XCTAssertEqual(decoded.volumes[1].error, "no disk matched")
        let old = try TalosJSON.decode(NodeVolumes.self, from: #"{"supported":false,"reason":"needs Talos v1.8 or newer","volumes":null}"#)
        XCTAssertEqual(old, NodeVolumes(supported: false, reason: "needs Talos v1.8 or newer"))
    }

    func testPaths() {
        XCTAssertEqual(normalizedPath("var/lib//"), "/var/lib")
        XCTAssertEqual(normalizedPath(""), "/")
        XCTAssertEqual(normalizedPath("/"), "/")
        XCTAssertEqual(parentPath("/var/lib"), "/var")
        XCTAssertEqual(parentPath("/var"), "/")
        XCTAssertEqual(parentPath("/"), "/")
        // Quick folders first; the ones that can take minutes last.
        XCTAssertEqual(diskUsageShortcuts.first, "/var/log")
        XCTAssertEqual(diskUsageShortcuts.suffix(2), ["/var", "/"])
        XCTAssertTrue(diskUsageShortcuts.allSatisfy { normalizedPath($0) == $0 })
    }

    func testBreadcrumb() {
        XCTAssertEqual(pathBreadcrumb("/"), [PathCrumb(name: "/", path: "/")])
        XCTAssertEqual(pathBreadcrumb("/var/lib/"), [PathCrumb(name: "/", path: "/"), PathCrumb(name: "var", path: "/var"),
                                                     PathCrumb(name: "lib", path: "/var/lib")])
        XCTAssertEqual(pathBreadcrumb("system//state").map(\.path), ["/", "/system", "/system/state"])
    }

    func testDiskUsageRows() throws {
        let usage = try TalosJSON.decode(DiskUsage.self, from: """
        {"entries":[{"path":"/var","size":1000,"isDir":true},{"path":"/var/log","size":100,"isDir":true},
                    {"path":"/var/lib/","size":800,"isDir":true},{"path":"/var/a.txt","size":100,"error":"permission denied"},
                    {"path":"/var/lib","size":800,"isDir":true}],"truncated":true}
        """)
        XCTAssertTrue(usage.truncated)
        let rows = diskUsageRows(usage.entries, root: "/var/")
        // Biggest first, then by path; the root itself and duplicates are left out.
        XCTAssertEqual(rows.map(\.name), ["lib", "a.txt", "log"])
        XCTAssertEqual(rows.map(\.path), ["/var/lib", "/var/a.txt", "/var/log"])
        XCTAssertEqual(rows.map(\.isDir), [true, false, true])
        XCTAssertEqual(rows.map(\.fraction), [1, 0.125, 0.125])
        XCTAssertEqual(rows.map(\.error), ["", "permission denied", ""])
        XCTAssertEqual(diskUsageTotal(usage.entries, root: "/var"), 1000)
        XCTAssertEqual(try TalosJSON.decode(DiskUsage.self, from: #"{"entries":null}"#).entries, [])
    }

    func testDiskUsageOfRootAndWithoutTotal() {
        let entries = [DiskUsageEntry(path: "/var", size: 30, isDir: true), DiskUsageEntry(path: "/system", size: 10, isDir: true),
                       DiskUsageEntry(path: "/var/lib", size: 20, isDir: true), DiskUsageEntry(path: "/empty", size: 0, isDir: true)]
        let rows = diskUsageRows(entries, root: "/")
        XCTAssertEqual(rows.map(\.name), ["var", "var/lib", "system", "empty"])
        XCTAssertEqual(rows.last?.fraction, 0)
        // No entry for the root: the direct children only (/var/lib is inside /var).
        XCTAssertEqual(diskUsageTotal(entries, root: "/"), 40)
        XCTAssertEqual(diskUsageRows([], root: "/var"), [])
        XCTAssertEqual(diskUsageTotal([], root: "/var"), 0)
    }
}
