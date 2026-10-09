package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/kube_flux.go and kube_flux_actions.go (the design is in
// the Linear plan document "D6. Flux"). Flux terms (Kustomization, HelmRelease, Ready, Stalled...) are its own names.

/** The Flux Kustomizations, HelmReleases and sources of every namespace. */
@Serializable
data class FluxStatus(
    /** False when the cluster serves neither Kustomizations nor HelmReleases. */
    val installed: Boolean = false,
    /** The Flux distribution ("v2.7.0"), else the kustomize-controller's tag; "" when unknown. */
    val version: String = "",
    /** Listing the HelmReleases failed; the rest still shows. */
    val helmError: String = "",
    /** Listing a source kind failed; the rest still shows. */
    val sourcesError: String = "",
    val apps: List<FluxApp> = emptyList(),
    val sources: List<FluxSource> = emptyList(),
)

/** A Kustomization or a HelmRelease. */
@Serializable
data class FluxApp(
    /** Kustomization or HelmRelease. */
    val kind: String = "",
    val namespace: String = "",
    val name: String,
    /** critical, warning, ok or idle (suspended): Ready, Stalled and Reconciling summed up. */
    val level: String = "",
    /** Bundled catalog icon id, "" when none. */
    val icon: String = "",
    /** Dashboard Icons slug, "" when none. */
    val remoteIcon: String = "",
    /** The Ready condition: True, False or Unknown. */
    val ready: String = "",
    val reason: String = "",
    val message: String = "",
    /** The controller is at it now. */
    val reconciling: Boolean = false,
    /** A requested reconcile was not handled yet. */
    val pending: Boolean = false,
    val stalled: Boolean = false,
    val suspended: Boolean = false,
    /** The Kustomization that applies this object from Git; null when none. */
    val owner: FluxRef? = null,
    val source: FluxRef? = null,
    val sourceURL: String = "",
    /** Kustomization only. */
    val path: String = "",
    /** HelmRelease only. */
    val chart: String = "",
    /** The wanted version (a semver range is fine), "" when the source decides. */
    val chartVersion: String = "",
    val targetNamespace: String = "",
    val interval: String = "",
    val prune: Boolean = false,
    /** What is applied: a Git revision, or the chart version of a release. */
    val revision: String = "",
    /** The last revision tried, which differs from [revision] while it fails. */
    val attemptedRevision: String = "",
    /** "namespace/name". */
    val dependsOn: List<String> = emptyList(),
    /** A release's install and upgrade failures in a row. */
    val failures: Int = 0,
    val conditions: List<FluxCondition> = emptyList(),
    /** A Kustomization's inventory. */
    val resources: List<FluxResource> = emptyList(),
    /** A HelmRelease's releases, newest first. */
    val history: List<FluxHistory> = emptyList(),
    /** Pods not ready in the namespaces it deploys to, only next to a failing or reconciling app. */
    val unhealthyPods: List<KubePod> = emptyList(),
    /** When Ready last changed, unix millis. */
    val reconciledAt: Long = 0,
) {
    /** Unique: a Kustomization and a HelmRelease may share a namespace and a name. */
    val key: String get() = fluxKey(kind, namespace, name)
    val serviceHealth: ServiceHealth get() = ServiceHealth.from(level)
    val state: FluxState get() = FluxState.of(serviceHealth, suspended, reconciling || pending)
    val isHelmRelease: Boolean get() = kind == FLUX_HELM_RELEASE
    val isKustomization: Boolean get() = kind == FLUX_KUSTOMIZATION

    /** The controller is working on it, or a reconcile was asked and not handled yet. */
    val isBusy: Boolean get() = reconciling || pending

    /** A suspended object refuses reconcile, force and reset: resume first. */
    val canReconcile: Boolean get() = !suspended

    /** Force and reset are a HelmRelease's only. */
    val canForce: Boolean get() = isHelmRelease && !suspended

    /** The revision tried last differs from the applied one: an upgrade is failing or under way. */
    val revisionDiffers: Boolean get() = attemptedRevision.isNotEmpty() && attemptedRevision != revision

    /** "chart@6.9.2" for a release, the short Git revision ("main@4be1d0c") for a Kustomization. */
    val versionLabel: String
        get() = when {
            isHelmRelease && chart.isNotEmpty() -> "$chart@${shortFluxRevision(revision).ifEmpty { chartVersion }}"
            else -> shortFluxRevision(revision)
        }

    /** Its inventory by kind, kinds sorted, names sorted within each. */
    val resourcesByKind: List<Pair<String, List<FluxResource>>>
        get() = resources.groupBy { it.kind }.toSortedMap().map { (kind, list) -> kind to list.sortedWith(compareBy({ it.namespace }, { it.name })) }
}

@Serializable
data class FluxRef(val kind: String = "", val namespace: String = "", val name: String = "") {
    val key: String get() = fluxKey(kind, namespace, name)
}

@Serializable
data class FluxCondition(
    val type: String = "",
    /** True, False or Unknown. */
    val status: String = "",
    val reason: String = "",
    val message: String = "",
    /** Unix millis. */
    val at: Long = 0,
)

@Serializable
data class FluxResource(
    val group: String = "",
    val kind: String = "",
    val namespace: String = "",
    val name: String = "",
) {
    val key: String get() = "$group/$kind/$namespace/$name"

    /** A Deployment, StatefulSet or DaemonSet: one a rollout restart applies to. */
    val restartable: Boolean get() = group == "apps" && kind in FLUX_RESTARTABLE_KINDS
}

private val FLUX_RESTARTABLE_KINDS = setOf("Deployment", "StatefulSet", "DaemonSet")

@Serializable
data class FluxHistory(
    /** The Helm release revision. */
    val version: Int = 0,
    val chartVersion: String = "",
    val appVersion: String = "",
    /** deployed, superseded, failed, uninstalled... */
    val status: String = "",
    /** Unix millis. */
    val deployedAt: Long = 0,
) {
    val failed: Boolean get() = status == "failed"
}

/** A GitRepository, OCIRepository, HelmRepository or Bucket. */
@Serializable
data class FluxSource(
    val kind: String = "",
    val namespace: String = "",
    val name: String,
    val level: String = "",
    val url: String = "",
    /** The branch, tag, semver range or digest followed. */
    val ref: String = "",
    /** The fetched artifact's: "main@sha1:…", a tag@digest, an index digest. */
    val revision: String = "",
    val ready: String = "",
    val reason: String = "",
    val message: String = "",
    val reconciling: Boolean = false,
    val pending: Boolean = false,
    val suspended: Boolean = false,
    val interval: String = "",
    /** Unix millis, 0 when never fetched. */
    val fetchedAt: Long = 0,
    /** The Kustomizations and HelmReleases using it. */
    val apps: Int = 0,
) {
    val key: String get() = fluxKey(kind, namespace, name)
    val serviceHealth: ServiceHealth get() = ServiceHealth.from(level)
    val isBusy: Boolean get() = reconciling || pending
}

/** Where an app or source stands, one word per object: what the overview counts. */
enum class FluxState {
    FAILING,
    RECONCILING,
    SUSPENDED,
    READY,
    ;

    companion object {
        /** Suspended wins (Flux ignores it), then critical; warning or work in progress reads as reconciling. */
        fun of(level: ServiceHealth, suspended: Boolean, busy: Boolean): FluxState = when {
            suspended || level == ServiceHealth.IDLE -> SUSPENDED
            level == ServiceHealth.CRITICAL -> FAILING
            busy || level == ServiceHealth.WARNING -> RECONCILING
            else -> READY
        }
    }
}

/** What KubeFluxAction runs, by its wire name. */
enum class FluxAction(val wire: String) {
    RECONCILE("reconcile"),
    /** Kustomization and HelmRelease only: fetches the source first. */
    RECONCILE_WITH_SOURCE("reconcileWithSource"),
    SUSPEND("suspend"),
    RESUME("resume"),
    /** HelmRelease only: a one-off upgrade even with nothing changed. */
    FORCE("force"),
    /** HelmRelease only: forgets the failure counts of a release that gave up. */
    RESET("reset"),
}

const val FLUX_KUSTOMIZATION = "Kustomization"
const val FLUX_HELM_RELEASE = "HelmRelease"

fun fluxKey(kind: String, namespace: String, name: String) = "$kind/$namespace/$name"

private val DIGEST = Regex("""^(sha1|sha256|sha384|sha512):([0-9a-f]+)$""")

/**
 * A Flux revision made short: "main@sha1:4be1d0c9…" reads "main@4be1d0c", "6.9.2@sha256:3b1f…"
 * "6.9.2@3b1f9c0", a bare digest its first 7 hex characters; versions and tags stay whole.
 */
fun shortFluxRevision(revision: String): String =
    revision.split('@').joinToString("@") { part ->
        DIGEST.matchEntire(part)?.groupValues?.get(2)?.take(7) ?: shortRevision(part)
    }

/** The inventory's catalog id for Flux. */
const val FLUX_CATALOG_ID = "flux"

/**
 * Flux's tile when the inventory has none to offer: the plain kubeconfig home, or the Talos
 * overview while its inventory still loads. Its icon is bundled (assets/appicons/flux.webp).
 */
val FLUX_TILE = InventoryApp(id = FLUX_CATALOG_ID, name = "Flux", category = "devops", icon = "flux", known = true)

/** Whether the inventory saw Flux running: only then is the cluster asked for its objects. */
val Inventory.hasFlux: Boolean get() = apps.any { it.id == FLUX_CATALOG_ID }
