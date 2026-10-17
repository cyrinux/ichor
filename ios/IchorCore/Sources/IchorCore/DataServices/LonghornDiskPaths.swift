import Foundation

/// The mount path every Longhorn disk shares, said once under the nodes instead of on each row;
/// nil when the disks sit on different paths or none has one.
public func longhornSharedDiskPath(_ nodes: [LonghornNode]) -> String? {
    let paths = Set(nodes.flatMap(\.disks).map(\.path).filter { !$0.isEmpty })
    return paths.count == 1 ? paths.first : nil
}

/// A disk's short name for its meter: the last segment of its path
/// ("/var/mnt/storage/longhorn" gives "longhorn"), the path itself when it has no segment.
public func longhornDiskLabel(_ path: String) -> String {
    path.split(separator: "/").last.map(String.init) ?? path
}
