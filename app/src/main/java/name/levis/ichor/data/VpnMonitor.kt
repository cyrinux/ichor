package name.levis.ichor.data

import android.content.Context
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import name.levis.ichor.R
import name.levis.ichor.ui.LocalizedException
import name.levis.ichor.ui.UiText

/** The cluster on screen is set to be reached over a VPN only, and none is up. */
class VpnRequiredException : LocalizedException(UiText.Res(R.string.common_vpn_required))

/** Whether this app's traffic goes through a VPN (its default network is one). */
class VpnMonitor(context: Context) {
    private val connectivity = context.getSystemService(ConnectivityManager::class.java)

    private val _up = MutableStateFlow(isUp())

    /** Follows the default network, so screens can reload once the VPN connects. */
    val up: StateFlow<Boolean> = _up.asStateFlow()

    init {
        connectivity.registerDefaultNetworkCallback(object : ConnectivityManager.NetworkCallback() {
            override fun onCapabilitiesChanged(network: Network, capabilities: NetworkCapabilities) {
                _up.value = capabilities.hasTransport(NetworkCapabilities.TRANSPORT_VPN)
            }

            override fun onLost(network: Network) {
                _up.value = isUp()
            }
        })
    }

    /** Asked at each call rather than read from [up]: the worker may run before the callback reported. */
    fun isUp(): Boolean = connectivity.getNetworkCapabilities(connectivity.activeNetwork)
        ?.hasTransport(NetworkCapabilities.TRANSPORT_VPN) == true
}
