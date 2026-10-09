package name.levis.ichor.data

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Deferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.async
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import name.levis.ichor.model.KubeActionAccess
import name.levis.ichor.model.KubePermission
import name.levis.ichor.model.KubeWhoAmI
import name.levis.ichor.model.effectiveAccess
import name.levis.ichorgo.Ichorgo

/**
 * "Can I?": which of the app's Kubernetes actions the active cluster's credentials may run,
 * asked before the tap (SelfSubjectAccessReviews) so a narrow OIDC, EKS or AKS role shows
 * a disabled action with its reason rather than a 403. The core keeps the answers a few
 * minutes; here, concurrent asks for one namespace share a single call.
 */
class KubePermissions(private val configs: ConfigRepository, private val kubeServers: KubeServers) {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private val lock = Mutex()
    private val inFlight = mutableMapOf<String, Deferred<KubeActionAccess>>()

    /**
     * The access to every action in [namespace] ("" cluster-wide): the cluster-wide answer
     * first, the namespace's own only when that one refuses something. Throws when the
     * cluster-wide one cannot be read; callers then offer every action.
     */
    suspend fun actionAccess(namespace: String): KubeActionAccess {
        val target = kubeServers.targetFor(configs.forCall())
        val wide = read(target, "")
        if (namespace.isEmpty()) return wide
        val local = if (wide.anyDenied) runCatching { read(target, namespace) }.getOrNull() else null
        return effectiveAccess(wide, local)
    }

    /** Whether the credentials may run [verb] on [resource] ("resource/subresource") of [group], in [namespace], on [name] ("" for any). */
    suspend fun can(verb: String, group: String, resource: String, namespace: String, name: String = ""): KubePermission {
        val target = kubeServers.targetFor(configs.forCall())
        return withContext(Dispatchers.IO) {
            TalosJson.decodeFromString(
                KubePermission.serializer(),
                Ichorgo.kubeCan(target.yaml, target.context, target.server, verb, group, resource, namespace, name),
            )
        }
    }

    /** Who the API server takes the credentials for. */
    suspend fun whoAmI(): KubeWhoAmI {
        val target = kubeServers.targetFor(configs.forCall())
        return withContext(Dispatchers.IO) {
            TalosJson.decodeFromString(KubeWhoAmI.serializer(), Ichorgo.kubeWhoAmI(target.yaml, target.context, target.server))
        }
    }

    private suspend fun read(target: KubeTarget, namespace: String): KubeActionAccess {
        // The credentials are part of the key: another cluster, or new ones, ask again.
        val key = "${target.yaml.hashCode()}\u0000${target.context}\u0000${target.server}\u0000$namespace"
        val call = lock.withLock {
            inFlight[key] ?: scope.async {
                try {
                    TalosJson.decodeFromString(
                        KubeActionAccess.serializer(),
                        Ichorgo.kubeActionAccess(target.yaml, target.context, target.server, namespace),
                    )
                } finally {
                    lock.withLock { inFlight.remove(key) }
                }
            }.also { inFlight[key] = it }
        }
        return call.await()
    }
}
