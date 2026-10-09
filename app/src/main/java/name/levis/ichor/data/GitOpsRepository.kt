package name.levis.ichor.data

import name.levis.ichor.model.ArgoAction
import name.levis.ichor.model.ArgoApp
import name.levis.ichor.model.ArgoFreezeAction
import name.levis.ichor.model.ArgoFreezeOptions
import name.levis.ichor.model.ArgoNetwork
import name.levis.ichor.model.ArgoStatus
import name.levis.ichor.model.ArgoSyncOptions
import name.levis.ichor.model.FluxAction
import name.levis.ichor.model.FluxDiff
import name.levis.ichor.model.FluxStatus
import name.levis.ichorgo.Ichorgo

/** Argo CD and Flux through their custom resources, on the Kubernetes API of the active cluster. */
class GitOpsRepository(go: GoCall) : GoRepository(go) {
    /**
     * Argo CD Applications, ApplicationSets and projects through their custom resources
     * (os:admin); `installed` is false without Argo CD.
     */
    suspend fun argoCD(): ArgoStatus = go.remember(ARGO_CD) {
        go.kube { cfg, ctx, server -> TalosJson.decodeFromString(ArgoStatus.serializer(), Ichorgo.kubeArgoCD(cfg, ctx, server)) }
    }

    /** Runs [action] on [app] (os:admin); [options] for a sync or a rollback. Throws when refused. */
    suspend fun argoAction(app: ArgoApp, action: ArgoAction, options: ArgoSyncOptions? = null) = go.kube { cfg, ctx, server ->
        val json = options?.let { TalosJson.encodeToString(ArgoSyncOptions.serializer(), it) }.orEmpty()
        Ichorgo.kubeArgoAction(cfg, ctx, server, app.namespace, app.name, action.wire, json)
    }

    /**
     * Changes the sync windows of the AppProject [namespace]/[project] (os:admin): freezes apps
     * for a while, extends or ends a freeze, removes a window, clears ended freezes. Throws when
     * refused.
     */
    suspend fun argoFreeze(namespace: String, project: String, action: ArgoFreezeAction, options: ArgoFreezeOptions = ArgoFreezeOptions()) =
        go.kube { cfg, ctx, server ->
            Ichorgo.kubeArgoFreeze(cfg, ctx, server, namespace, project, action.wire, TalosJson.encodeToString(ArgoFreezeOptions.serializer(), options))
        }

    /**
     * How traffic reaches [app] (os:admin): hosts, Gateways, routes, Services, pods and nodes.
     * Never cached: the app detail asks again whenever it reloads the app.
     */
    suspend fun argoNetwork(app: ArgoApp): ArgoNetwork = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(ArgoNetwork.serializer(), Ichorgo.kubeArgoNetwork(cfg, ctx, server, app.namespace, app.name))
    }

    /**
     * Flux Kustomizations, HelmReleases and sources through their custom resources (os:admin);
     * `installed` is false without Flux.
     */
    suspend fun flux(): FluxStatus = go.remember(FLUX) {
        go.kube { cfg, ctx, server -> TalosJson.decodeFromString(FluxStatus.serializer(), Ichorgo.kubeFlux(cfg, ctx, server)) }
    }

    /** Runs [action] on the Flux object [kind] [namespace]/[name] (os:admin). Throws when refused. */
    suspend fun fluxAction(kind: String, namespace: String, name: String, action: FluxAction) = go.kube { cfg, ctx, server ->
        Ichorgo.kubeFluxAction(cfg, ctx, server, kind, namespace, name, action.wire)
    }

    /**
     * What reconciling the Flux object [kind] [namespace]/[name] now would change, object by
     * object (os:admin, read only: server-side apply dry runs). Not cached: always fresh.
     */
    suspend fun fluxDiff(kind: String, namespace: String, name: String): FluxDiff = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(FluxDiff.serializer(), Ichorgo.kubeFluxDiff(cfg, ctx, server, kind, namespace, name))
    }

    /**
     * What syncing the Argo CD Application [namespace]/[name] now would change, object by
     * object (os:admin, read only): what the application controller compared last, read from
     * Argo CD's Redis through a port-forward. Not cached on the phone: always fresh.
     */
    suspend fun argoDiff(namespace: String, name: String): FluxDiff = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(FluxDiff.serializer(), Ichorgo.kubeArgoDiff(cfg, ctx, server, namespace, name))
    }
}
