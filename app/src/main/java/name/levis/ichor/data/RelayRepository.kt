package name.levis.ichor.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import name.levis.ichor.model.PromSource
import name.levis.ichor.model.RelayDevice
import name.levis.ichor.model.RelayDiscovery
import name.levis.ichor.model.RelayPairing
import name.levis.ichor.model.RelayPayload
import name.levis.ichor.model.RelayRegistered
import name.levis.ichor.model.toGoJson
import name.levis.ichorgo.Ichorgo

/**
 * The in-cluster ichor-relay, reached through the Kubernetes API service proxy: Go reads the
 * pairing token from the relay's Secret with this config's own rights and never hands it over.
 * Nothing is cached; pairing is refused in the demo and recorded in the audit log.
 */
class RelayRepository(go: GoCall) : GoRepository(go) {
    /** The relays among the cluster's Services, and whether this config may pair with them. */
    suspend fun discover(): RelayDiscovery = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(RelayDiscovery.serializer(), Ichorgo.relayDiscover(cfg, ctx, server))
    }

    /** The relay's VAPID public key, for the distributor's registration. */
    suspend fun pairing(source: PromSource): RelayPairing = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(RelayPairing.serializer(), Ichorgo.relayPairing(cfg, ctx, server, source.toGoJson()))
    }

    /** Registers [device] with the relay at [source]: the device's id, the same when pairing again. */
    suspend fun register(source: PromSource, device: RelayDevice): String = go.kube { cfg, ctx, server ->
        val json = Ichorgo.relayRegister(cfg, ctx, server, source.toGoJson(), TalosJson.encodeToString(RelayDevice.serializer(), device))
        TalosJson.decodeFromString(RelayRegistered.serializer(), json).id
    }

    /** Removes the device [id] from the relay at [source]. */
    suspend fun unregister(source: PromSource, id: String) {
        go.kube { cfg, ctx, server -> Ichorgo.relayUnregister(cfg, ctx, server, source.toGoJson(), id) }
    }

    companion object {
        /** Checks a payload the distributor decrypted; throws when it is not one the relay sends. */
        suspend fun decode(payload: String): RelayPayload = withContext(Dispatchers.IO) {
            TalosJson.decodeFromString(RelayPayload.serializer(), Ichorgo.relayDecodePayload(payload))
        }
    }
}
