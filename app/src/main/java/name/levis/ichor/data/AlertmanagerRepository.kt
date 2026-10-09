package name.levis.ichor.data

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.builtins.MapSerializer
import kotlinx.serialization.builtins.serializer
import name.levis.ichor.model.AmAlerts
import name.levis.ichor.model.AmMatcher
import name.levis.ichor.model.AmSilences
import name.levis.ichor.model.PromDiscovery
import name.levis.ichor.model.PromSource
import name.levis.ichor.model.toGoJson
import name.levis.ichorgo.Ichorgo

/**
 * Alertmanager, reached like a metrics source ([PromSource] of kind "alertmanager"): its
 * alerts and silences, and silencing or expiring from the phone. Results stay in memory only.
 */
class AlertmanagerRepository(go: GoCall) : GoRepository(go) {
    /** The Alertmanagers among the cluster's Services, the likeliest first. */
    suspend fun discover(): List<PromSource> = go.remember(AM_DISCOVERY) {
        go.kube { cfg, ctx, server -> TalosJson.decodeFromString(PromDiscovery.serializer(), Ichorgo.alertmanagerDiscover(cfg, ctx, server)).sources }
    }

    /**
     * The alerts of [source], grouped by alertname. [active], [silenced] and [inhibited] pick the
     * states (all three for the screen and the card, whose counts cover every alert).
     */
    suspend fun alerts(source: PromSource, active: Boolean = true, silenced: Boolean = true, inhibited: Boolean = true): AmAlerts =
        go.remember(alertsKey(source, active, silenced, inhibited)) {
            go.kube { cfg, ctx, server ->
                val json = Ichorgo.alertmanagerAlerts(cfg, ctx, server, source.toGoJson(), active, silenced, inhibited, "", "")
                TalosJson.decodeFromString(AmAlerts.serializer(), json)
            }
        }

    /** The active and pending silences of [source]; [withExpired] adds the latest expired ones. */
    suspend fun silences(source: PromSource, withExpired: Boolean): AmSilences = go.remember(silencesKey(source, withExpired)) {
        go.kube { cfg, ctx, server ->
            TalosJson.decodeFromString(AmSilences.serializer(), Ichorgo.alertmanagerSilences(cfg, ctx, server, source.toGoJson(), withExpired))
        }
    }

    /** The matchers silencing exactly the alert of [labels] (its HA replica labels left out). */
    suspend fun silenceMatchers(labels: Map<String, String>): List<AmMatcher> = withContext(Dispatchers.IO) {
        val json = Ichorgo.alertmanagerSilenceMatchers(TalosJson.encodeToString(LABELS, labels))
        TalosJson.decodeFromString(MATCHERS, json)
    }

    /** Silences [matchers] on [source] for [minutes] from now, with [comment]: the new silence's ID. Throws when refused. */
    suspend fun silence(source: PromSource, matchers: List<AmMatcher>, minutes: Long, comment: String): String = go.kube { cfg, ctx, server ->
        Ichorgo.alertmanagerSilence(cfg, ctx, server, source.toGoJson(), TalosJson.encodeToString(MATCHERS, matchers), minutes, comment)
    }.also { forget(source) }

    /** Ends the silence [id] on [source] now. Throws when refused. */
    suspend fun expire(source: PromSource, id: String) {
        go.kube { cfg, ctx, server -> Ichorgo.alertmanagerExpire(cfg, ctx, server, source.toGoJson(), id) }
        forget(source)
    }

    /** The alerts and silences cached for [source]: a change shows on the next read. */
    private fun forget(source: PromSource) = go.results.forgetContaining(sourceKey(source))

    private companion object {
        const val AM_DISCOVERY = "am|discovery"
        val LABELS = MapSerializer(String.serializer(), String.serializer())
        val MATCHERS = ListSerializer(AmMatcher.serializer())

        fun sourceKey(source: PromSource) = "am|${source.mode}|${source.label}|${source.tenant}|"

        fun alertsKey(source: PromSource, active: Boolean, silenced: Boolean, inhibited: Boolean) =
            sourceKey(source) + "alerts|$active$silenced$inhibited"

        fun silencesKey(source: PromSource, withExpired: Boolean) = sourceKey(source) + "silences|$withExpired"
    }
}

/**
 * The Alertmanager of the cluster [fingerprint]: the one the user chose ([store]), else the
 * likeliest one found in the cluster (not saved, so a later install is found too); null when
 * none. A discovery that fails (the role may not list Services) finds none.
 */
suspend fun AlertmanagerRepository.sourceFor(store: AlertmanagerStore, fingerprint: String): PromSource? {
    val chosen = withContext(Dispatchers.IO) { store.read(fingerprint) }.source
    return chosen ?: try {
        discover().firstOrNull()
    } catch (e: CancellationException) {
        throw e
    } catch (_: Exception) {
        null
    }
}
