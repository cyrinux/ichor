import Foundation

// A value every row repeats says nothing about the row: the Capacity workload list hides a
// workload's priority class, priority and QoS when they equal what most of the list has.

/// The value more than half of `values` share; nil for an empty list or when none does.
public func majorityValue<T: Hashable>(_ values: [T]) -> T? {
    let counts = Dictionary(values.map { ($0, 1) }, uniquingKeysWith: +)
    guard let top = counts.max(by: { $0.value < $1.value }), top.value * 2 > values.count else { return nil }
    return top.key
}

/// The priority class ("" for none), priority and QoS most of a workload list shares.
public struct CapacityMajority: Equatable, Sendable {
    public let priorityClass: String?
    public let priority: Int?
    public let qos: QosClass?

    public init(_ workloads: [CapacityWorkload]) {
        priorityClass = majorityValue(workloads.map(\.classKey))
        priority = majorityValue(workloads.map(\.priority))
        qos = majorityValue(workloads.map(\.qos))
    }
}

/// One part of a workload's secondary line, in the order shown.
public enum CapacityWorkloadDetail: Equatable, Sendable {
    case noPriorityClass
    case priorityClass(String)
    case priority(Int)
    case qos(QosClass)
}

public extension CapacityWorkload {
    /// Its class as the majority rule compares it: "" when it has none.
    var classKey: String { noPriorityClass ? "" : priorityClassName }

    /// Its class, priority and QoS, each left out when it equals the list's majority (an
    /// unknown QoS is never shown).
    func details(against majority: CapacityMajority) -> [CapacityWorkloadDetail] {
        let showsClass = classKey != majority.priorityClass
        let classPart: CapacityWorkloadDetail? =
            if !showsClass {
                nil
            } else if noPriorityClass {
                .noPriorityClass
            } else if priorityClassName.isEmpty {
                nil
            } else {
                .priorityClass(priorityClassName)
            }
        let priorityPart: CapacityWorkloadDetail? = priority != majority.priority ? .priority(priority) : nil
        let qosPart: CapacityWorkloadDetail? = qos != .unknown && qos != majority.qos ? .qos(qos) : nil
        return [classPart, priorityPart, qosPart].compactMap { $0 }
    }
}

/// The one problem a node row shows, worst first.
public enum CapacityNodeFlag: Equatable, Sendable {
    case overcommitted, diskPressure, diskLow
}

public extension CapacityNode {
    /// Overcommitted before DiskPressure before disk low; nil when none applies.
    var flag: CapacityNodeFlag? {
        if overcommitted { return .overcommitted }
        if diskPressure { return .diskPressure }
        if diskLow { return .diskLow }
        return nil
    }

    /// "control-plane · garage, longhorn": control-plane first, the other roles in the order
    /// given; "" without roles.
    var roleLine: String {
        let others = roles.filter { $0 != "control-plane" }.joined(separator: ", ")
        let parts = (roles.contains("control-plane") ? ["control-plane"] : []) + (others.isEmpty ? [] : [others])
        return parts.joined(separator: " · ")
    }
}

public extension Array where Element == CapacityNode {
    /// Overcommitted nodes first, each group in the order given.
    var overcommittedFirst: [CapacityNode] {
        filter(\.overcommitted) + filter { !$0.overcommitted }
    }
}
