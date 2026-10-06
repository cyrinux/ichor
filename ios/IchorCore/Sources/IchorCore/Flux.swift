import Foundation

// Mirrors go/ichorgo/kube_flux.go and kube_flux_actions.go (the wire format and the UX are
// described in plans/flux/README.md). The logic on top lives in FluxLogic.swift.

/// The catalog id of Flux in the inventory: only clusters running it are asked (KubeFlux).
public let fluxCatalogID = "flux"

/// The Flux Kustomizations, HelmReleases and sources of every namespace, read through their
/// custom resources (os:admin).
public struct FluxStatus: Decodable, Equatable, Sendable {
    /// False when the cluster serves neither Kustomizations nor HelmReleases.
    public let installed: Bool
    /// The Flux distribution ("v2.7.0"), else the kustomize-controller's tag; "" when unknown.
    public let version: String
    /// Listing the HelmReleases (or the sources) failed; the rest still shows.
    public let helmError: String
    public let sourcesError: String
    /// Kustomizations and HelmReleases, worst level first, then by name.
    public let apps: [FluxApp]
    public let sources: [FluxSource]

    public init(installed: Bool = true, version: String = "", helmError: String = "", sourcesError: String = "",
                apps: [FluxApp] = [], sources: [FluxSource] = []) {
        self.installed = installed
        self.version = version
        self.helmError = helmError
        self.sourcesError = sourcesError
        self.apps = apps
        self.sources = sources
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        installed = try c.field(.installed, false)
        version = try c.field(.version, "")
        helmError = try c.field(.helmError, "")
        sourcesError = try c.field(.sourcesError, "")
        apps = try c.field(.apps, [])
        sources = try c.field(.sources, [])
    }

    private enum CodingKeys: String, CodingKey { case installed, version, helmError, sourcesError, apps, sources }
}

/// A Kustomization or a HelmRelease.
public struct FluxApp: Decodable, Equatable, Identifiable, Sendable {
    /// Kustomization or HelmRelease.
    public let kind: String
    public let namespace: String
    public let name: String
    /// Ready, Stalled and Reconciling summed up: critical, warning, ok or idle (suspended).
    public let level: ServiceHealth
    /// Bundled catalog icon, "" when none.
    public let icon: String
    /// Dashboard Icons slug, "" when none.
    public let remoteIcon: String
    /// The Ready condition: True, False or Unknown.
    public let ready: String
    public let reason: String
    public let message: String
    /// The controller is at it now.
    public let reconciling: Bool
    /// A requested reconcile was not handled yet.
    public let pending: Bool
    public let stalled: Bool
    public let suspended: Bool
    /// The Kustomization that applies it from Git, nil when none.
    public let owner: FluxRef?
    public let source: FluxRef?
    public let sourceURL: String
    /// Kustomization only.
    public let path: String
    /// HelmRelease only.
    public let chart: String
    /// The wanted chart version (a semver range is fine), "" when the source decides.
    public let chartVersion: String
    public let targetNamespace: String
    public let interval: String
    public let prune: Bool
    /// What is applied (a Git revision, or a release's chart version).
    public let revision: String
    /// The last one tried, which differs while it fails.
    public let attemptedRevision: String
    /// "namespace/name".
    public let dependsOn: [String]
    /// A release's install and upgrade failures in a row.
    public let failures: Int
    public let conditions: [FluxCondition]
    /// A Kustomization's inventory.
    public let resources: [FluxResource]
    /// A HelmRelease's releases, newest first.
    public let history: [FluxHistory]
    /// Pods not ready where it deploys (sent for failing or reconciling apps only).
    public let unhealthyPods: [KubePod]
    /// When Ready last changed, unix ms.
    public let reconciledAt: Int64

    public var id: String { "\(kind)/\(namespace)/\(name)" }
    /// What KubeFluxAction is called with.
    public var target: FluxRef { FluxRef(kind: kind, namespace: namespace, name: name) }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kind = try c.field(.kind, "")
        namespace = try c.field(.namespace, "")
        name = try c.decode(String.self, forKey: .name)
        level = try c.wire(.level)
        icon = try c.field(.icon, "")
        remoteIcon = try c.field(.remoteIcon, "")
        ready = try c.field(.ready, "")
        reason = try c.field(.reason, "")
        message = try c.field(.message, "")
        reconciling = try c.field(.reconciling, false)
        pending = try c.field(.pending, false)
        stalled = try c.field(.stalled, false)
        suspended = try c.field(.suspended, false)
        owner = try c.decodeIfPresent(FluxRef.self, forKey: .owner)
        source = try c.decodeIfPresent(FluxRef.self, forKey: .source)
        sourceURL = try c.field(.sourceURL, "")
        path = try c.field(.path, "")
        chart = try c.field(.chart, "")
        chartVersion = try c.field(.chartVersion, "")
        targetNamespace = try c.field(.targetNamespace, "")
        interval = try c.field(.interval, "")
        prune = try c.field(.prune, false)
        revision = try c.field(.revision, "")
        attemptedRevision = try c.field(.attemptedRevision, "")
        dependsOn = try c.field(.dependsOn, [])
        failures = try c.field(.failures, 0)
        conditions = try c.field(.conditions, [])
        resources = try c.field(.resources, [])
        history = try c.field(.history, [])
        unhealthyPods = try c.field(.unhealthyPods, [])
        reconciledAt = try c.field(.reconciledAt, 0)
    }

    private enum CodingKeys: String, CodingKey {
        case kind, namespace, name, level, icon, remoteIcon, ready, reason, message, reconciling, pending, stalled, suspended
        case owner, source, sourceURL, path, chart, chartVersion, targetNamespace, interval, prune, revision, attemptedRevision
        case dependsOn, failures, conditions, resources, history, unhealthyPods, reconciledAt
    }
}

/// A Flux object by kind, namespace and name.
public struct FluxRef: Decodable, Equatable, Hashable, Sendable {
    public let kind: String
    public let namespace: String
    public let name: String

    public init(kind: String, namespace: String, name: String) {
        self.kind = kind
        self.namespace = namespace
        self.name = name
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kind = try c.field(.kind, "")
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
    }

    /// "GitRepository flux-system/flux-system".
    public var label: String { "\(kind) \(namespace)/\(name)" }

    private enum CodingKeys: String, CodingKey { case kind, namespace, name }
}

public struct FluxCondition: Decodable, Equatable, Sendable {
    /// Ready, Reconciling, Stalled, Healthy, Released...
    public let type: String
    /// True, False or Unknown.
    public let status: String
    public let reason: String
    public let message: String
    /// Unix ms.
    public let at: Int64

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        type = try c.field(.type, "")
        status = try c.field(.status, "")
        reason = try c.field(.reason, "")
        message = try c.field(.message, "")
        at = try c.field(.at, 0)
    }

    private enum CodingKeys: String, CodingKey { case type, status, reason, message, at }
}

/// An object a Kustomization applied (its inventory).
public struct FluxResource: Decodable, Equatable, Identifiable, Sendable {
    public let group: String
    public let kind: String
    /// "" for cluster-scoped kinds.
    public let namespace: String
    public let name: String

    public var id: String { "\(group)/\(kind)/\(namespace)/\(name)" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        group = try c.field(.group, "")
        kind = try c.field(.kind, "")
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
    }

    private enum CodingKeys: String, CodingKey { case group, kind, namespace, name }
}

/// One Helm release of a HelmRelease.
public struct FluxHistory: Decodable, Equatable, Identifiable, Sendable {
    /// The Helm release revision.
    public let version: Int
    public let chartVersion: String
    public let appVersion: String
    /// deployed, superseded, failed, uninstalled...
    public let status: String
    /// Unix ms.
    public let deployedAt: Int64

    public var id: Int { version }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        version = try c.field(.version, 0)
        chartVersion = try c.field(.chartVersion, "")
        appVersion = try c.field(.appVersion, "")
        status = try c.field(.status, "")
        deployedAt = try c.field(.deployedAt, 0)
    }

    private enum CodingKeys: String, CodingKey { case version, chartVersion, appVersion, status, deployedAt }
}

/// A GitRepository, OCIRepository, HelmRepository or Bucket.
public struct FluxSource: Decodable, Equatable, Identifiable, Sendable {
    public let kind: String
    public let namespace: String
    public let name: String
    public let level: ServiceHealth
    public let url: String
    /// The branch, tag, semver range or digest followed.
    public let ref: String
    /// The fetched artifact's ("main@sha1:…", a tag@digest, an index digest).
    public let revision: String
    /// True, False, Unknown, or "" for an OCI HelmRepository (nothing to fetch).
    public let ready: String
    public let reason: String
    public let message: String
    public let reconciling: Bool
    public let pending: Bool
    public let suspended: Bool
    public let interval: String
    /// Unix ms, 0 when nothing was fetched.
    public let fetchedAt: Int64
    /// The Kustomizations and HelmReleases using it.
    public let apps: Int

    public var id: String { "\(kind)/\(namespace)/\(name)" }
    /// What KubeFluxAction is called with.
    public var target: FluxRef { FluxRef(kind: kind, namespace: namespace, name: name) }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kind = try c.field(.kind, "")
        namespace = try c.field(.namespace, "")
        name = try c.decode(String.self, forKey: .name)
        level = try c.wire(.level)
        url = try c.field(.url, "")
        ref = try c.field(.ref, "")
        revision = try c.field(.revision, "")
        ready = try c.field(.ready, "")
        reason = try c.field(.reason, "")
        message = try c.field(.message, "")
        reconciling = try c.field(.reconciling, false)
        pending = try c.field(.pending, false)
        suspended = try c.field(.suspended, false)
        interval = try c.field(.interval, "")
        fetchedAt = try c.field(.fetchedAt, 0)
        apps = try c.field(.apps, 0)
    }

    private enum CodingKeys: String, CodingKey {
        case kind, namespace, name, level, url, ref, revision, ready, reason, message, reconciling, pending, suspended
        case interval, fetchedAt, apps
    }
}

/// The actions KubeFluxAction runs; the raw values are the wire names.
public enum FluxAction: String, Sendable, CaseIterable {
    /// Any kind.
    case reconcile
    /// Kustomization and HelmRelease: their source first, then them.
    case reconcileWithSource
    /// Any kind: spec.suspend.
    case suspend, resume
    /// HelmRelease only: a one-off upgrade even with nothing changed.
    case force
    /// HelmRelease only: forget the failure counts, retry from scratch.
    case reset
}
