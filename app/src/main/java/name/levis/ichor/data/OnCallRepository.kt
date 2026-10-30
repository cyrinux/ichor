package name.levis.ichor.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.builtins.ListSerializer
import name.levis.ichor.TalosApp
import name.levis.ichor.model.AmAlerts
import name.levis.ichor.model.OnCallAlertInput
import name.levis.ichor.model.OnCallConfig
import name.levis.ichor.model.OnCallIdentity
import name.levis.ichor.model.OnCallIncidents
import name.levis.ichor.model.toGoJson
import name.levis.ichorgo.Ichorgo

/** The pager of the cluster on screen; nothing to keep between calls. */
val TalosApp.onCallRepository: OnCallRepository get() = OnCallRepository(goCall)

/** The pager behind a cluster's alerts: results stay in memory only, the settings come from [OnCallStore]. */
class OnCallRepository(go: GoCall) : GoRepository(go) {
    /** The open incidents of [config], matched to [alerts]. The demo answers without settings. */
    suspend fun incidents(config: OnCallConfig, alerts: AmAlerts): OnCallIncidents = go.kube { cfg, ctx, _ ->
        val inputs = alerts.groups.flatMap { group -> group.alerts.map { OnCallAlertInput(it.fingerprint, it.labels) } }
        val json = Ichorgo.onCallIncidents(cfg, ctx, config.toGoJson(), TalosJson.encodeToString(INPUTS, inputs))
        TalosJson.decodeFromString(OnCallIncidents.serializer(), json)
    }

    /** Acknowledges or resolves ([name.levis.ichor.model.OnCallVerb]) the incident [id]. Throws when refused. */
    suspend fun act(config: OnCallConfig, id: String, verb: String) {
        go.kube { cfg, ctx, _ -> Ichorgo.onCallAct(cfg, ctx, config.toGoJson(), id, verb) }
    }

    /** Who [config]'s token is. Throws when PagerDuty refuses it. */
    suspend fun test(config: OnCallConfig): OnCallIdentity = withContext(Dispatchers.IO) {
        TalosJson.decodeFromString(OnCallIdentity.serializer(), Ichorgo.onCallTest(config.toGoJson()))
    }

    private companion object {
        val INPUTS = ListSerializer(OnCallAlertInput.serializer())
    }
}
