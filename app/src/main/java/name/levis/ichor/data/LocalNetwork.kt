package name.levis.ichor.data

import android.content.Context
import android.content.pm.PackageManager
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import android.os.Build
import name.levis.ichor.model.LocalAddress
import java.net.Inet4Address
import java.net.Inet6Address

/**
 * Android 17's local network permission: from it on, an app targeting it reaches hosts on
 * the Wi-Fi/Ethernet network (private and link-local addresses) only once the user allowed
 * it. Traffic through a VPN is not concerned. Not a constant of older SDKs, hence the string.
 */
const val LOCAL_NETWORK_PERMISSION = "android.permission.ACCESS_LOCAL_NETWORK"
private const val LOCAL_NETWORK_PERMISSION_SDK = 37

/** Whether the system asks for [LOCAL_NETWORK_PERMISSION] on this device. */
fun localNetworkPermissionNeeded(): Boolean = Build.VERSION.SDK_INT >= LOCAL_NETWORK_PERMISSION_SDK

fun hasLocalNetworkAccess(context: Context): Boolean =
    !localNetworkPermissionNeeded() ||
        context.checkSelfPermission(LOCAL_NETWORK_PERMISSION) == PackageManager.PERMISSION_GRANTED

/**
 * The phone's IPv4 and IPv6 addresses on its Wi-Fi and Ethernet networks (not mobile data,
 * not a VPN). Link-local IPv6 addresses are left out: reaching a host on them needs a zone.
 */
fun localAddresses(context: Context): List<LocalAddress> {
    val connectivity = context.getSystemService(ConnectivityManager::class.java) ?: return emptyList()
    @Suppress("DEPRECATION") // allNetworks: the replacement needs a callback, for a one-off lookup
    return connectivity.allNetworks.filter { network ->
        val caps = connectivity.getNetworkCapabilities(network) ?: return@filter false
        !caps.hasTransport(NetworkCapabilities.TRANSPORT_VPN) &&
            (caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) || caps.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET))
    }.flatMap { network ->
        connectivity.getLinkProperties(network)?.linkAddresses.orEmpty()
            .filter { it.address is Inet4Address || (it.address is Inet6Address && !it.address.isLinkLocalAddress) }
            .mapNotNull { link -> link.address.hostAddress?.let { LocalAddress(it.substringBefore('%'), link.prefixLength) } }
    }
}

/** Whether the active network is metered (mobile data, a hotspot): large lists load less eagerly. */
fun isMeteredNetwork(context: Context): Boolean =
    context.getSystemService(ConnectivityManager::class.java)?.isActiveNetworkMetered ?: false
