package name.levis.ichor.data

import name.levis.ichor.model.ArgoRollout
import name.levis.ichor.model.ArgoRolloutAction
import name.levis.ichor.model.ArgoRolloutsStatus
import name.levis.ichorgo.Ichorgo

/** Argo Rollouts (canary and blue-green) and the kubectl plugin's actions, through the Kubernetes API. */
class ArgoRolloutsRepository(go: GoCall) : GoRepository(go) {
    /** Every Rollout of the cluster; installed false when it does not serve them. */
    suspend fun rollouts(): ArgoRolloutsStatus = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(ArgoRolloutsStatus.serializer(), Ichorgo.kubeArgoRollouts(cfg, ctx, server))
    }

    /** Runs [action] on [rollout] with the plugin's patches; the Go core refuses what the state forbids. */
    suspend fun act(rollout: ArgoRollout, action: ArgoRolloutAction) {
        go.kube { cfg, ctx, server -> Ichorgo.kubeArgoRolloutAction(cfg, ctx, server, rollout.namespace, rollout.name, action.wire) }
    }
}
