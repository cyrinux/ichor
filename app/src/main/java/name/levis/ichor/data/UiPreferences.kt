package name.levis.ichor.data

import android.content.Context
import android.content.SharedPreferences
import androidx.annotation.StringRes
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import name.levis.ichor.R
import name.levis.ichor.model.KubeObjectAction
import name.levis.ichor.model.KubeObjectBar
import name.levis.ichor.model.KubernetesAction
import name.levis.ichor.model.KubernetesBar
import name.levis.ichor.model.OverviewAction
import name.levis.ichor.model.OverviewBar
import name.levis.ichor.model.OverviewCard
import name.levis.ichor.model.OverviewLayout
import name.levis.ichor.model.KubeHomeAction
import name.levis.ichor.model.KubeHomeBar
import name.levis.ichor.model.KubeHomeCard
import name.levis.ichor.model.KubeHomeLayout

enum class ThemeMode(@StringRes val label: Int) {
    AUTO(R.string.settings_theme_auto),
    LIGHT(R.string.settings_theme_light),
    DARK(R.string.settings_theme_dark),
    BLACK(R.string.settings_theme_black),
    ;

    /** Whether this mode renders dark, given the system setting (used by AUTO). */
    fun isDark(systemDark: Boolean): Boolean = when (this) {
        AUTO -> systemDark
        LIGHT -> false
        DARK, BLACK -> true
    }

    companion object {
        fun parse(name: String?): ThemeMode = entries.firstOrNull { it.name == name } ?: AUTO
    }
}

/**
 * Screenshot mode: the Go core replaces IPs, hostnames, context names and [extraWords] in
 * everything it returns, so screens can be shared without leaking the cluster's identity.
 */
data class PrivacyMask(val enabled: Boolean = false, val extraWords: String = "") {
    /** [extraWords] as the Go core takes them: trimmed, comma-separated, no empty entries. */
    val words: String
        get() = extraWords.split(',').map { it.trim() }.filter { it.isNotEmpty() }.distinct().joinToString(",")

    /** Keeps what is saved while masked apart from the real data: "real", or a hash of the words. */
    val storageKey: String
        get() = if (enabled) java.security.MessageDigest.getInstance("SHA-256").digest(words.toByteArray()).take(8).joinToString("") { "%02x".format(it) } else "real"
}

class UiPreferences(private val prefs: SharedPreferences) {
    private val _themeMode = MutableStateFlow(ThemeMode.parse(prefs.getString(KEY_THEME, null)))
    val themeMode: StateFlow<ThemeMode> = _themeMode.asStateFlow()

    /** When the app lock is on, screenshots and the recents preview are blocked unless this is set. */
    private val _allowScreenshots = MutableStateFlow(prefs.getBoolean(KEY_SCREENSHOTS, false))
    val allowScreenshots: StateFlow<Boolean> = _allowScreenshots.asStateFlow()

    /** In-app language as a BCP-47 tag; "" follows the system. */
    private val _language = MutableStateFlow(prefs.getString(KEY_LANGUAGE, "").orEmpty())
    val language: StateFlow<String> = _language.asStateFlow()

    /** Refreshes the overview's CPU and memory every few seconds while it is on screen. */
    private val _liveClusterStats = MutableStateFlow(prefs.getBoolean(KEY_LIVE_CLUSTER_STATS, true))
    val liveClusterStats: StateFlow<Boolean> = _liveClusterStats.asStateFlow()

    /**
     * Downloads icons the app does not bundle from jsDelivr (Dashboard Icons). Off by default:
     * a third-party request, even if only the public icon name is sent.
     */
    private val _remoteAppIcons = MutableStateFlow(prefs.getBoolean(KEY_REMOTE_APP_ICONS, false))
    val remoteAppIcons: StateFlow<Boolean> = _remoteAppIcons.asStateFlow()

    /**
     * Keeps the last results of each cluster on disk, encrypted, to show them when it cannot
     * be reached. Off by default: otherwise cluster data never leaves memory.
     */
    private val _offlineCache = MutableStateFlow(prefs.getBoolean(KEY_OFFLINE_CACHE, false))
    val offlineCache: StateFlow<Boolean> = _offlineCache.asStateFlow()

    /** The overview's cards: their order and which are hidden (long-press a card to change). */
    private val _overviewLayout = MutableStateFlow(OverviewCard.layout.parse(prefs.getString(KEY_OVERVIEW_LAYOUT, null)))
    val overviewLayout: StateFlow<OverviewLayout> = _overviewLayout.asStateFlow()

    /** The Kubernetes home's cards (a cluster added from a kubeconfig), arranged the same way. */
    private val _kubeHomeLayout = MutableStateFlow(KubeHomeCard.layout.parse(prefs.getString(KEY_KUBE_HOME_LAYOUT, null)))
    val kubeHomeLayout: StateFlow<KubeHomeLayout> = _kubeHomeLayout.asStateFlow()

    /** The overview's nodes card with a full row per node; collapsed (a chip each) by default. */
    private val _nodesExpanded = MutableStateFlow(prefs.getBoolean(KEY_NODES_EXPANDED, false))
    val nodesExpanded: StateFlow<Boolean> = _nodesExpanded.asStateFlow()
    /** The overview's app-bar actions: their order and which are icons or in its menu. */
    private val _overviewBar = MutableStateFlow(OverviewAction.bar.parse(prefs.getString(KEY_OVERVIEW_BAR, null)))
    val overviewBar: StateFlow<OverviewBar> = _overviewBar.asStateFlow()

    /** The Kubernetes home's app-bar actions, arranged the same way. */
    private val _kubeHomeBar = MutableStateFlow(KubeHomeAction.bar.parse(prefs.getString(KEY_KUBE_HOME_BAR, null)))
    val kubeHomeBar: StateFlow<KubeHomeBar> = _kubeHomeBar.asStateFlow()

    /** The Kubernetes screen's app-bar actions, arranged the same way. */
    private val _kubernetesBar = MutableStateFlow(KubernetesAction.bar.parse(prefs.getString(KEY_KUBERNETES_BAR, null)))
    val kubernetesBar: StateFlow<KubernetesBar> = _kubernetesBar.asStateFlow()

    /** A Kubernetes object's app-bar actions, one bar for every kind. */
    private val _kubeObjectBar = MutableStateFlow(KubeObjectAction.bar.parse(prefs.getString(KEY_KUBE_OBJECT_BAR, null)))
    val kubeObjectBar: StateFlow<KubeObjectBar> = _kubeObjectBar.asStateFlow()

    private val _privacyMask = MutableStateFlow(
        PrivacyMask(prefs.getBoolean(KEY_PRIVACY_MASK, false), prefs.getString(KEY_PRIVACY_WORDS, "").orEmpty()),
    )
    val privacyMask: StateFlow<PrivacyMask> = _privacyMask.asStateFlow()

    /** Committed synchronously: the worker and widget read it back at process start. */
    fun setPrivacyMask(mask: PrivacyMask) {
        prefs.edit().putBoolean(KEY_PRIVACY_MASK, mask.enabled).putString(KEY_PRIVACY_WORDS, mask.extraWords).commit()
        _privacyMask.value = mask
    }

    fun setAllowScreenshots(allow: Boolean) {
        prefs.edit().putBoolean(KEY_SCREENSHOTS, allow).apply()
        _allowScreenshots.value = allow
    }

    fun setLiveClusterStats(enabled: Boolean) {
        prefs.edit().putBoolean(KEY_LIVE_CLUSTER_STATS, enabled).apply()
        _liveClusterStats.value = enabled
    }

    fun setRemoteAppIcons(enabled: Boolean) {
        prefs.edit().putBoolean(KEY_REMOTE_APP_ICONS, enabled).apply()
        _remoteAppIcons.value = enabled
    }

    fun setOfflineCache(enabled: Boolean) {
        prefs.edit().putBoolean(KEY_OFFLINE_CACHE, enabled).apply()
        _offlineCache.value = enabled
    }

    fun setOverviewLayout(layout: OverviewLayout) {
        prefs.edit().putString(KEY_OVERVIEW_LAYOUT, layout.encode()).apply()
        _overviewLayout.value = layout
    }

    fun setKubeHomeLayout(layout: KubeHomeLayout) {
        prefs.edit().putString(KEY_KUBE_HOME_LAYOUT, layout.encode()).apply()
        _kubeHomeLayout.value = layout
    }

    fun setKubeHomeBar(bar: KubeHomeBar) {
        prefs.edit().putString(KEY_KUBE_HOME_BAR, bar.encode()).apply()
        _kubeHomeBar.value = bar
    }

    fun setNodesExpanded(expanded: Boolean) {
        prefs.edit().putBoolean(KEY_NODES_EXPANDED, expanded).apply()
        _nodesExpanded.value = expanded
    }

    fun setOverviewBar(bar: OverviewBar) {
        prefs.edit().putString(KEY_OVERVIEW_BAR, bar.encode()).apply()
        _overviewBar.value = bar
    }

    fun setKubernetesBar(bar: KubernetesBar) {
        prefs.edit().putString(KEY_KUBERNETES_BAR, bar.encode()).apply()
        _kubernetesBar.value = bar
    }

    fun setKubeObjectBar(bar: KubeObjectBar) {
        prefs.edit().putString(KEY_KUBE_OBJECT_BAR, bar.encode()).apply()
        _kubeObjectBar.value = bar
    }

    /** How much [screen] scales its dense monospace text (logs, packets): 1 is the theme size. */
    fun monoTextScale(screen: String): Float = prefs.getFloat(KEY_MONO_TEXT_SCALE + screen, 1f)

    fun setMonoTextScale(screen: String, factor: Float) {
        prefs.edit().putFloat(KEY_MONO_TEXT_SCALE + screen, factor).apply()
    }

    fun setThemeMode(mode: ThemeMode) {
        prefs.edit().putString(KEY_THEME, mode.name).apply()
        _themeMode.value = mode
    }

    /** Committed synchronously: the activity is recreated right after and reads it back. */
    fun setLanguage(tag: String) {
        prefs.edit().putString(KEY_LANGUAGE, tag).commit()
        _language.value = tag
    }

    companion object {
        const val FILE = "ichor-ui"
        private const val KEY_THEME = "theme_mode"
        private const val KEY_SCREENSHOTS = "allow_screenshots"
        private const val KEY_LANGUAGE = "language"
        private const val KEY_PRIVACY_MASK = "privacy_mask"
        private const val KEY_PRIVACY_WORDS = "privacy_mask_words"
        private const val KEY_LIVE_CLUSTER_STATS = "live_cluster_stats"
        private const val KEY_REMOTE_APP_ICONS = "remote_app_icons"
        private const val KEY_OFFLINE_CACHE = "offline_cache"
        private const val KEY_OVERVIEW_LAYOUT = "overview_layout"
        private const val KEY_NODES_EXPANDED = "overview_nodes_expanded"
        private const val KEY_OVERVIEW_BAR = "overview_bar"
        private const val KEY_KUBE_HOME_LAYOUT = "kube_home_layout"
        private const val KEY_KUBE_HOME_BAR = "kube_home_bar"
        private const val KEY_KUBERNETES_BAR = "kubernetes_bar"
        private const val KEY_KUBE_OBJECT_BAR = "kube_object_bar"
        private const val KEY_MONO_TEXT_SCALE = "mono_text_scale_"

        /** Reads the language without the app singletons (usable from attachBaseContext). */
        fun storedLanguage(context: Context): String =
            context.getSharedPreferences(FILE, Context.MODE_PRIVATE).getString(KEY_LANGUAGE, "").orEmpty()
    }
}
