package name.levis.ichor.data

import android.content.Context
import android.content.SharedPreferences
import androidx.annotation.StringRes
import androidx.core.content.edit
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
import name.levis.ichor.model.ImageScanner
import name.levis.ichor.model.RecentTarget
import name.levis.ichor.model.RecentTargets
import name.levis.ichor.model.ShareTarget
import name.levis.ichor.model.withRecent

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

/** The design language that draws the chrome (cards, bars, buttons), independent of [ThemeMode]. */
enum class UiStyle(@StringRes val label: Int) {
    MATERIAL3(R.string.settings_ui_style_material3),
    EXPRESSIVE(R.string.settings_ui_style_expressive),
    ;

    companion object {
        fun parse(name: String?): UiStyle = entries.firstOrNull { it.name == name } ?: MATERIAL3
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

    private val _uiStyle = MutableStateFlow(UiStyle.parse(prefs.getString(KEY_UI_STYLE, null)))
    val uiStyle: StateFlow<UiStyle> = _uiStyle.asStateFlow()

    /** When the app lock is on, screenshots and the recents preview are blocked unless this is set. */
    private val _allowScreenshots = MutableStateFlow(prefs.getBoolean(KEY_SCREENSHOTS, false))
    val allowScreenshots: StateFlow<Boolean> = _allowScreenshots.asStateFlow()

    /** In-app language as a BCP-47 tag; "" follows the system. */
    private val _language = MutableStateFlow(prefs.getString(KEY_LANGUAGE, "").orEmpty())
    val language: StateFlow<String> = _language.asStateFlow()

    /** Refreshes the overview's CPU and memory every few seconds while it is on screen. */
    private val _liveClusterStats = MutableStateFlow(prefs.getBoolean(KEY_LIVE_CLUSTER_STATS, true))
    val liveClusterStats: StateFlow<Boolean> = _liveClusterStats.asStateFlow()

    /** Shows the "Add to home screen" buttons and menu entries; on by default. */
    private val _addToHome = MutableStateFlow(prefs.getBoolean(KEY_ADD_TO_HOME, true))
    val addToHome: StateFlow<Boolean> = _addToHome.asStateFlow()

    /**
     * Downloads icons the app does not bundle from jsDelivr (Dashboard Icons). Off by default:
     * a third-party request, even if only the public icon name is sent.
     */
    private val _remoteAppIcons = MutableStateFlow(prefs.getBoolean(KEY_REMOTE_APP_ICONS, false))
    val remoteAppIcons: StateFlow<Boolean> = _remoteAppIcons.asStateFlow()

    /** The plugin index Settings → Plugins browses; "": the project's. */
    private val _pluginIndexUrl = MutableStateFlow(prefs.getString(KEY_PLUGIN_INDEX_URL, "").orEmpty())
    val pluginIndexUrl: StateFlow<String> = _pluginIndexUrl.asStateFlow()

    /** The tags the fleet screen is filtered by ("" for the untagged clusters); none: every cluster. */
    private val _fleetTagFilter = MutableStateFlow(prefs.getStringSet(KEY_FLEET_TAG_FILTER, null).orEmpty().toSet())
    val fleetTagFilter: StateFlow<Set<String>> = _fleetTagFilter.asStateFlow()

    /** Logs where a cold start's time goes to logcat (tag Ichor.Startup). Off by default. */
    private val _startupTrace = MutableStateFlow(prefs.getBoolean(KEY_STARTUP_TRACE, false))
    val startupTrace: StateFlow<Boolean> = _startupTrace.asStateFlow()

    /** Keeps the core's redacted diagnostics log in the data directory across launches. Off by default. */
    private val _diagPersist = MutableStateFlow(prefs.getBoolean(KEY_DIAG_PERSIST, false))
    val diagPersist: StateFlow<Boolean> = _diagPersist.asStateFlow()

    /** The time (unix ms) of the newest exit record of the OS already copied to the diagnostics log. */
    var lastExitRecordAt: Long
        get() = prefs.getLong(KEY_LAST_EXIT_RECORD_AT, 0)
        set(value) = prefs.edit { putLong(KEY_LAST_EXIT_RECORD_AT, value) }

    /**
     * Offers a root shell on the nodes of a cluster without Talos, through a privileged pod
     * (`kubectl debug node/`). Off by default: it is root on the node.
     */
    private val _nodeDebugShell = MutableStateFlow(prefs.getBoolean(KEY_NODE_DEBUG_SHELL, false))
    val nodeDebugShell: StateFlow<Boolean> = _nodeDebugShell.asStateFlow()

    /** The vulnerability scanner an image scan runs in the cluster: Grype by default. */
    private val _imageScanner = MutableStateFlow(ImageScanner.parse(prefs.getString(KEY_IMAGE_SCANNER, null)))
    val imageScanner: StateFlow<ImageScanner> = _imageScanner.asStateFlow()

    /**
     * Keeps the last results of each cluster on disk, encrypted, to show them when it cannot
     * be reached, and at once at launch. On by default (a launch shows the home from it); a
     * user who turned it off keeps it off.
     */
    private val _offlineCache = MutableStateFlow(prefs.getBoolean(KEY_OFFLINE_CACHE, true))
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

    /**
     * The last searches across clusters, latest first; they may name pods or apps, so they are
     * dropped with the last known state (screenshot mode changed, every cluster removed).
     */
    private val _recentSearches = MutableStateFlow(
        prefs.getString(KEY_RECENT_SEARCHES, null).orEmpty().split('\n').filter { it.isNotBlank() },
    )
    val recentSearches: StateFlow<List<String>> = _recentSearches.asStateFlow()

    /**
     * The objects opened lately, newest first (the search's "Recently opened"); dropped with
     * the recent searches, and never recorded in screenshot mode (labels are stored as shown).
     */
    private val _recentTargets = MutableStateFlow(RecentTargets.parse(prefs.getString(KEY_RECENT_TARGETS, null)))
    val recentTargets: StateFlow<List<RecentTarget>> = _recentTargets.asStateFlow()

    private val _privacyMask = MutableStateFlow(
        PrivacyMask(prefs.getBoolean(KEY_PRIVACY_MASK, false), prefs.getString(KEY_PRIVACY_WORDS, "").orEmpty()),
    )
    val privacyMask: StateFlow<PrivacyMask> = _privacyMask.asStateFlow()

    /**
     * The demo tour (coach marks on the Talos demo home) is due: set when the demo is first
     * added, or replayed from Settings; cleared once skipped or finished.
     */
    private val _tourPending = MutableStateFlow(prefs.getBoolean(KEY_TOUR_PENDING, false))
    val tourPending: StateFlow<Boolean> = _tourPending.asStateFlow()

    /**
     * The home offers background alerts once (a card), after the first real cluster is added:
     * pending until turned on or dismissed. Never for the demo.
     */
    private val _alertsOfferPending = MutableStateFlow(prefs.getBoolean(KEY_ALERTS_OFFER_PENDING, false))
    val alertsOfferPending: StateFlow<Boolean> = _alertsOfferPending.asStateFlow()

    /** Offers alerts the first time a real cluster is added; later imports do not bring it back. */
    fun offerAlertsOnce() {
        if (prefs.getBoolean(KEY_ALERTS_OFFERED, false)) return
        setAlertsOfferPending(true)
    }

    fun setAlertsOfferPending(pending: Boolean) {
        prefs.edit().putBoolean(KEY_ALERTS_OFFER_PENDING, pending).putBoolean(KEY_ALERTS_OFFERED, true).apply()
        _alertsOfferPending.value = pending
    }

    /** Shows the demo tour the first time a demo is added; later demos do not bring it back. */
    fun offerDemoTour() {
        if (prefs.getBoolean(KEY_TOUR_OFFERED, false)) return
        setTourPending(true)
    }

    fun setTourPending(pending: Boolean) {
        prefs.edit().putBoolean(KEY_TOUR_PENDING, pending).putBoolean(KEY_TOUR_OFFERED, true).apply()
        _tourPending.value = pending
    }

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

    fun setAddToHome(enabled: Boolean) {
        prefs.edit().putBoolean(KEY_ADD_TO_HOME, enabled).apply()
        _addToHome.value = enabled
    }

    fun setFleetTagFilter(tags: Set<String>) {
        prefs.edit().putStringSet(KEY_FLEET_TAG_FILTER, tags).apply()
        _fleetTagFilter.value = tags
    }

    fun setPluginIndexUrl(url: String) {
        prefs.edit().putString(KEY_PLUGIN_INDEX_URL, url).apply()
        _pluginIndexUrl.value = url
    }

    fun setRemoteAppIcons(enabled: Boolean) {
        prefs.edit().putBoolean(KEY_REMOTE_APP_ICONS, enabled).apply()
        _remoteAppIcons.value = enabled
    }

    /**
     * The app-wide "Record actions in the cluster" switch of older versions, read once and
     * removed (it is per cluster now, in [AuditEventClusters]); null when it was never set.
     */
    fun takeLegacyAuditEvents(): Boolean? {
        if (!prefs.contains(KEY_AUDIT_EVENTS)) return null
        val on = prefs.getBoolean(KEY_AUDIT_EVENTS, false)
        prefs.edit().remove(KEY_AUDIT_EVENTS).commit()
        return on
    }

    fun setStartupTrace(enabled: Boolean) {
        prefs.edit().putBoolean(KEY_STARTUP_TRACE, enabled).apply()
        _startupTrace.value = enabled
    }

    fun setDiagPersist(enabled: Boolean) {
        prefs.edit { putBoolean(KEY_DIAG_PERSIST, enabled) }
        _diagPersist.value = enabled
    }

    fun setNodeDebugShell(enabled: Boolean) {
        prefs.edit().putBoolean(KEY_NODE_DEBUG_SHELL, enabled).apply()
        _nodeDebugShell.value = enabled
    }

    fun setImageScanner(scanner: ImageScanner) {
        prefs.edit().putString(KEY_IMAGE_SCANNER, scanner.id).apply()
        _imageScanner.value = scanner
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

    fun addRecentSearch(query: String) = setRecentSearches(_recentSearches.value.withRecent(query))

    fun removeRecentSearch(query: String) = setRecentSearches(_recentSearches.value - query)

    fun clearRecentSearches() {
        setRecentSearches(emptyList())
        setRecentTargets(emptyList())
    }

    /** Records [target], shown as [label] and [detail], as opened now; nothing in screenshot mode. */
    fun addRecentTarget(target: ShareTarget, label: String, detail: String = "") {
        if (_privacyMask.value.enabled) return
        setRecentTargets(RecentTargets.adding(_recentTargets.value, RecentTarget(target, label, detail, System.currentTimeMillis())))
    }

    fun removeRecentTarget(target: ShareTarget) = setRecentTargets(RecentTargets.removing(_recentTargets.value, target))

    private fun setRecentTargets(recent: List<RecentTarget>) {
        if (recent == _recentTargets.value) return
        prefs.edit().putString(KEY_RECENT_TARGETS, RecentTargets.encode(recent)).apply()
        _recentTargets.value = recent
    }

    private fun setRecentSearches(queries: List<String>) {
        if (queries == _recentSearches.value) return
        prefs.edit().putString(KEY_RECENT_SEARCHES, queries.joinToString("\n")).apply()
        _recentSearches.value = queries
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

    fun setUiStyle(style: UiStyle) {
        prefs.edit().putString(KEY_UI_STYLE, style.name).apply()
        _uiStyle.value = style
    }

    /** Committed synchronously: the activity is recreated right after and reads it back. */
    fun setLanguage(tag: String) {
        prefs.edit().putString(KEY_LANGUAGE, tag).commit()
        _language.value = tag
    }

    companion object {
        const val FILE = "ichor-ui"
        private const val KEY_THEME = "theme_mode"
        private const val KEY_UI_STYLE = "ui_style"
        private const val KEY_SCREENSHOTS = "allow_screenshots"
        private const val KEY_LANGUAGE = "language"
        private const val KEY_PRIVACY_MASK = "privacy_mask"
        private const val KEY_PRIVACY_WORDS = "privacy_mask_words"
        private const val KEY_LIVE_CLUSTER_STATS = "live_cluster_stats"
        private const val KEY_ADD_TO_HOME = "add_to_home"
        private const val KEY_REMOTE_APP_ICONS = "remote_app_icons"
        private const val KEY_PLUGIN_INDEX_URL = "plugin_index_url"
        private const val KEY_FLEET_TAG_FILTER = "fleet_tag_filter"
        private const val KEY_AUDIT_EVENTS = "audit_events"
        private const val KEY_STARTUP_TRACE = "startup_trace"
        private const val KEY_DIAG_PERSIST = "diag_persist"
        private const val KEY_LAST_EXIT_RECORD_AT = "last_exit_record_at"
        private const val KEY_NODE_DEBUG_SHELL = "node_debug_shell"
        private const val KEY_IMAGE_SCANNER = "image_scanner"
        private const val KEY_OFFLINE_CACHE = "offline_cache"
        private const val KEY_OVERVIEW_LAYOUT = "overview_layout"
        private const val KEY_NODES_EXPANDED = "overview_nodes_expanded"
        private const val KEY_OVERVIEW_BAR = "overview_bar"
        private const val KEY_KUBE_HOME_LAYOUT = "kube_home_layout"
        private const val KEY_KUBE_HOME_BAR = "kube_home_bar"
        private const val KEY_KUBERNETES_BAR = "kubernetes_bar"
        private const val KEY_KUBE_OBJECT_BAR = "kube_object_bar"
        private const val KEY_MONO_TEXT_SCALE = "mono_text_scale_"
        private const val KEY_RECENT_SEARCHES = "recent_searches"
        private const val KEY_RECENT_TARGETS = "recent_targets"
        private const val KEY_TOUR_PENDING = "demo_tour_pending"
        private const val KEY_TOUR_OFFERED = "demo_tour_offered"
        private const val KEY_ALERTS_OFFER_PENDING = "alerts_offer_pending"
        private const val KEY_ALERTS_OFFERED = "alerts_offered"

        /** Reads the language without the app singletons (usable from attachBaseContext). */
        fun storedLanguage(context: Context): String =
            context.getSharedPreferences(FILE, Context.MODE_PRIVATE).getString(KEY_LANGUAGE, "").orEmpty()
    }
}
