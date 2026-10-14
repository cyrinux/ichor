import Foundation

// Data services: restoring a Velero backup (see Velero.swift, kube_velero_restore.go).

/// What restoring one backup holds, whether it can be, and the Restores of it already there.
public struct VeleroRestorePreview: Decodable, Equatable, Sendable {
    public let namespace: String
    public let backup: String
    /// The Backup's own: Completed, PartiallyFailed, Failed, FailedValidation, InProgress…
    public let phase: String
    public let completedAt: Int64
    /// Unix ms, 0 when it never expires.
    public let expiresAt: Int64
    public let storageLocation: String
    public let locationAvailable: Bool
    /// Empty or "*": every namespace.
    public let includedNamespaces: [String]
    public let excludedNamespaces: [String]
    public let includedResources: [String]
    /// The Backup's own: "true", "false" or "unset".
    public let includeClusterResources: String
    public let snapshotVolumes: Bool?
    public let defaultVolumesToFsBackup: Bool?
    public let itemsBackedUp: Int
    public let totalItems: Int
    public let volumeSnapshots: Int
    public let errors: Int
    public let warnings: Int
    public let canRestore: Bool
    /// Why it cannot (or had better not) be restored.
    public let reasons: [VeleroRestoreReason]
    /// The Restores of this backup, newest first; the first one running is the one being followed.
    public let restores: [VeleroRestoreDetail]
    /// The Restores or the storage locations could not be read: the backup still shows.
    public let error: String

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        backup = try c.field(.backup, "")
        phase = try c.field(.phase, "")
        completedAt = try c.field(.completedAt, 0)
        expiresAt = try c.field(.expiresAt, 0)
        storageLocation = try c.field(.storageLocation, "")
        locationAvailable = try c.field(.locationAvailable, false)
        includedNamespaces = try c.field(.includedNamespaces, [])
        excludedNamespaces = try c.field(.excludedNamespaces, [])
        includedResources = try c.field(.includedResources, [])
        includeClusterResources = try c.field(.includeClusterResources, "unset")
        snapshotVolumes = try c.decodeIfPresent(Bool.self, forKey: .snapshotVolumes)
        defaultVolumesToFsBackup = try c.decodeIfPresent(Bool.self, forKey: .defaultVolumesToFsBackup)
        itemsBackedUp = try c.field(.itemsBackedUp, 0)
        totalItems = try c.field(.totalItems, 0)
        volumeSnapshots = try c.field(.volumeSnapshots, 0)
        errors = try c.field(.errors, 0)
        warnings = try c.field(.warnings, 0)
        canRestore = try c.field(.canRestore, false)
        reasons = try c.wireList(.reasons)
        restores = try c.field(.restores, [])
        error = try c.field(.error, "")
    }

    private enum CodingKeys: String, CodingKey {
        case namespace, backup, phase, completedAt, expiresAt, storageLocation, locationAvailable
        case includedNamespaces, excludedNamespaces, includedResources, includeClusterResources
        case snapshotVolumes, defaultVolumesToFsBackup, itemsBackedUp, totalItems, volumeSnapshots
        case errors, warnings, canRestore, reasons, restores, error
    }
}

/// One Restore of the backup: the progress the apps follow after launching one.
public struct VeleroRestoreDetail: Decodable, Equatable, Identifiable, Sendable {
    public let name: String
    /// New, InProgress, WaitingForPluginOperations…, Finalizing…, Completed, PartiallyFailed, Failed.
    public let phase: String
    public let startedAt: Int64
    public let completedAt: Int64
    public let errors: Int
    public let warnings: Int
    public let failureReason: String
    /// What the restore controller refused, when it set FailedValidation.
    public let validationErrors: [String]
    public let itemsRestored: Int
    public let totalItems: Int
    public let namespaceMapping: [String: String]
    public let includedNamespaces: [String]
    public let existingResourcePolicy: String

    public var id: String { name }
    public var running: Bool {
        phase == "New" || phase == "InProgress" || phase.hasPrefix("WaitingForPluginOperations") || phase.hasPrefix("Finalizing")
    }
    public var finished: Bool {
        phase == "Completed" || phase == "PartiallyFailed" || phase == "Failed" || phase == "FailedValidation"
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        phase = try c.field(.phase, "")
        startedAt = try c.field(.startedAt, 0)
        completedAt = try c.field(.completedAt, 0)
        errors = try c.field(.errors, 0)
        warnings = try c.field(.warnings, 0)
        failureReason = try c.field(.failureReason, "")
        validationErrors = try c.field(.validationErrors, [])
        itemsRestored = try c.field(.itemsRestored, 0)
        totalItems = try c.field(.totalItems, 0)
        namespaceMapping = try c.field(.namespaceMapping, [:])
        includedNamespaces = try c.field(.includedNamespaces, [])
        existingResourcePolicy = try c.field(.existingResourcePolicy, "")
    }

    private enum CodingKeys: String, CodingKey {
        case name, phase, startedAt, completedAt, errors, warnings, failureReason, validationErrors
        case itemsRestored, totalItems, namespaceMapping, includedNamespaces, existingResourcePolicy
    }
}

/// Why a backup cannot (or had better not) be restored, as the Go core names it.
public enum VeleroRestoreReason: String, Sendable {
    case partiallyFailed, locationUnavailable, expired, restoreRunning, allNamespaces
}

/// The options of a restore, serialized to JSON for the Go core (gomobile passes strings only).
public struct VeleroRestoreOptions: Encodable, Equatable, Sendable {
    /// Empty: every namespace of the backup.
    public var includedNamespaces: [String]
    /// Source namespace → target namespace; a target that does not exist is created.
    public var namespaceMapping: [String: String]
    /// "none": leave existing objects; "update": overwrite them.
    public var existingResourcePolicy: String
    /// nil: Velero's default (restore them).
    public var restorePVs: Bool?
    public var includeClusterResources: Bool?

    public init(includedNamespaces: [String] = [], namespaceMapping: [String: String] = [:],
                existingResourcePolicy: String = "none", restorePVs: Bool? = nil, includeClusterResources: Bool? = nil) {
        self.includedNamespaces = includedNamespaces
        self.namespaceMapping = namespaceMapping
        self.existingResourcePolicy = existingResourcePolicy
        self.restorePVs = restorePVs
        self.includeClusterResources = includeClusterResources
    }

    private enum CodingKeys: String, CodingKey {
        case includedNamespaces, namespaceMapping, existingResourcePolicy, restorePVs, includeClusterResources
    }

    public func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        // Empty and unset options are left out, so the core keeps Velero's defaults.
        if !includedNamespaces.isEmpty { try c.encode(includedNamespaces, forKey: .includedNamespaces) }
        if !namespaceMapping.isEmpty { try c.encode(namespaceMapping, forKey: .namespaceMapping) }
        if !existingResourcePolicy.isEmpty { try c.encode(existingResourcePolicy, forKey: .existingResourcePolicy) }
        try c.encodeIfPresent(restorePVs, forKey: .restorePVs)
        try c.encodeIfPresent(includeClusterResources, forKey: .includeClusterResources)
    }
}
