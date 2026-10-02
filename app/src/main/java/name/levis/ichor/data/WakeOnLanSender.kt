package name.levis.ichor.data

import android.content.Context
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.withContext
import name.levis.ichor.model.WolTarget
import name.levis.ichor.model.directedBroadcast
import name.levis.ichor.model.magicPacket
import name.levis.ichor.model.parseMac
import java.net.DatagramPacket
import java.net.DatagramSocket
import java.net.Inet4Address
import java.net.InetAddress

/**
 * Sends Wake-on-LAN magic packets straight from the phone (no Talos API involved: the node
 * is off). The phone has to reach [WolTarget.address]: the node's LAN, or a router or relay
 * that forwards the packet there.
 */
class WakeOnLanSender(private val context: Context) {

    /** What was sent to: the given address, or the local network's broadcast one. */
    suspend fun send(target: WolTarget): String = withContext(Dispatchers.IO) {
        val mac = requireNotNull(parseMac(target.mac)) { "invalid MAC address ${target.mac}" }
        val payload = magicPacket(mac)
        // No address given means "the network the phone is on": the Wi-Fi/Ethernet one, even
        // when Android routes through mobile data (a LAN without internet) or a VPN.
        val local = if (target.broadcast.isBlank()) localNetwork() else null
        val destination = local?.let(::broadcastOf) ?: target.address
        val address = InetAddress.getByName(destination)
        DatagramSocket().use { socket ->
            local?.bindSocket(socket)
            socket.broadcast = true
            repeat(COPIES) { i ->
                if (i > 0) delay(GAP_MS)
                socket.send(DatagramPacket(payload, payload.size, address, target.port))
            }
        }
        destination
    }

    private fun localNetwork(): Network? {
        val connectivity = context.getSystemService(ConnectivityManager::class.java) ?: return null
        @Suppress("DEPRECATION") // allNetworks: the replacement needs a callback, for a one-off lookup
        return connectivity.allNetworks.firstOrNull { network ->
            val caps = connectivity.getNetworkCapabilities(network) ?: return@firstOrNull false
            caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) || caps.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET)
        }
    }

    private fun broadcastOf(network: Network): String? {
        val connectivity = context.getSystemService(ConnectivityManager::class.java) ?: return null
        val link = connectivity.getLinkProperties(network)?.linkAddresses?.firstOrNull { it.address is Inet4Address }
            ?: return null
        return directedBroadcast(link.address.address, link.prefixLength)
    }

    private companion object {
        /** UDP gives no delivery report: a few copies, a little apart, make a lost one harmless. */
        const val COPIES = 3
        const val GAP_MS = 100L
    }
}
