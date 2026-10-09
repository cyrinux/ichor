import Foundation

// Data services: Velero schedules, backups and locations (see DataServices.swift).

public struct VeleroStatus: Decodable, Equatable, Sendable {
    public let version: String
    public let error: String
    /// Problems first.
    public let schedules: [VeleroSchedule]
    /// Backups taken by hand (no schedule) that failed in the last week, newest first.
    public let adhoc: [VeleroAdhocBackup]
    /// Unavailable first.
    public let locations: [VeleroLocation]

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        version = try c.field(.version, "")
        error = try c.field(.error, "")
        schedules = try c.field(.schedules, [])
        adhoc = try c.field(.adhoc, [])
        locations = try c.field(.locations, [])
    }

    private enum CodingKeys: String, CodingKey { case version, error, schedules, adhoc, locations }
}

public struct VeleroSchedule: Decodable, Equatable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    /// Cron, as written.
    public let schedule: String
    public let paused: Bool
    /// The schedule's own: New, Enabled or FailedValidation.
    public let phase: String
    public let validationErrors: [String]
    public let health: ServiceHealth
    /// Known reasons only; values from newer cores are dropped.
    public let reasons: [VeleroReason]
    public let storageLocation: String
    /// Empty or "*": every namespace.
    public let includedNamespaces: [String]
    /// The latest finished backup, nil when none is left.
    public let lastBackup: VeleroBackup?
    /// The latest Completed backup (unix ms), 0 when none.
    public let lastSuccessAt: Int64
    public let inProgress: Bool

    public var id: String { label }
    public var label: String { "\(namespace)/\(name)" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        schedule = try c.field(.schedule, "")
        paused = try c.field(.paused, false)
        phase = try c.field(.phase, "")
        validationErrors = try c.field(.validationErrors, [])
        health = try c.wire(.health)
        reasons = try c.wireList(.reasons)
        storageLocation = try c.field(.storageLocation, "")
        includedNamespaces = try c.field(.includedNamespaces, [])
        lastBackup = try c.decodeIfPresent(VeleroBackup.self, forKey: .lastBackup)
        lastSuccessAt = try c.field(.lastSuccessAt, 0)
        inProgress = try c.field(.inProgress, false)
    }

    private enum CodingKeys: String, CodingKey {
        case namespace, name, schedule, paused, phase, validationErrors, health, reasons, storageLocation, includedNamespaces
        case lastBackup, lastSuccessAt, inProgress
    }
}

public struct VeleroBackup: Decodable, Equatable, Sendable {
    public let name: String
    /// Completed, PartiallyFailed, Failed or FailedValidation.
    public let phase: String
    public let startedAt: Int64
    public let completedAt: Int64
    public let errors: Int
    public let warnings: Int
    public let failureReason: String

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        phase = try c.field(.phase, "")
        startedAt = try c.field(.startedAt, 0)
        completedAt = try c.field(.completedAt, 0)
        errors = try c.field(.errors, 0)
        warnings = try c.field(.warnings, 0)
        failureReason = try c.field(.failureReason, "")
    }

    private enum CodingKeys: String, CodingKey { case name, phase, startedAt, completedAt, errors, warnings, failureReason }
}

/// A backup taken by hand that failed: a warning.
public struct VeleroAdhocBackup: Decodable, Equatable, Identifiable, Sendable {
    public let namespace: String
    public let backup: VeleroBackup
    public let storageLocation: String
    public let health: ServiceHealth

    public var id: String { label }
    public var label: String { "\(namespace)/\(backup.name)" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        // The Go core inlines the backup's fields.
        backup = try VeleroBackup(from: decoder)
        storageLocation = try c.field(.storageLocation, "")
        health = try c.wire(.health)
    }

    private enum CodingKeys: String, CodingKey { case namespace, storageLocation, health }
}

public struct VeleroLocation: Decodable, Equatable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    public let provider: String
    public let bucket: String
    public let isDefault: Bool
    /// Available or Unavailable, "" before the first check.
    public let phase: String
    public let message: String
    public let lastValidatedAt: Int64
    public let health: ServiceHealth

    public var id: String { label }
    public var label: String { "\(namespace)/\(name)" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        provider = try c.field(.provider, "")
        bucket = try c.field(.bucket, "")
        isDefault = try c.field(.isDefault, false)
        phase = try c.field(.phase, "")
        message = try c.field(.message, "")
        lastValidatedAt = try c.field(.lastValidatedAt, 0)
        health = try c.wire(.health)
    }

    private enum CodingKeys: String, CodingKey {
        case namespace, name, provider, bucket, phase, message, lastValidatedAt, health
        case isDefault = "default"
    }
}

/// Why a Velero schedule is not ok, as the Go core names it.
public enum VeleroReason: String, Sendable {
    case failed, location, partiallyFailed, stale, invalid
}
