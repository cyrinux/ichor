package name.levis.ichor.model

import kotlinx.serialization.Serializable

/** PluginUserList: the plugins the user imported, sorted by id. */
@Serializable
data class PluginUserList(val items: List<PluginUserItem> = emptyList())

/**
 * An imported plugin as PluginUserImport and PluginUserList give it: what it reads and can do,
 * whether it is on, and [error], why it cannot be turned on (a format this app does not read, a
 * file changed since the import), "" when it can.
 */
@Serializable
data class PluginUserItem(
    val id: String = "",
    val name: String = "",
    val website: String = "",
    val version: String = "",
    val resources: List<PluginCatalogResource> = emptyList(),
    val actions: List<PluginCatalogAction> = emptyList(),
    val enabled: Boolean = false,
    val error: String = "",
    val importedAt: Long = 0,
    /** From the check: an imported plugin has this id, and importing replaces it. */
    val replaces: Boolean = false,
) {
    /** It can be turned on: it loads in this app. */
    val usable: Boolean get() = error.isEmpty()
}

/** A resource line of the consent sheet: "sleepinfos.kube-green.com" (or "secrets"), and whether it is a Secret shown in clear. */
data class PluginConsentResource(val name: String, val kind: String, val secret: Boolean, val revealed: Boolean)

/** An action line of the consent sheet: its label, and "patch sleepinfos" for what it needs. */
data class PluginConsentAction(val label: String, val verb: String, val needs: String)

/**
 * What the consent sheet shows before an imported plugin is turned on: what it reads, which
 * Secrets it shows in clear (flagged), and what it can change with which verbs.
 */
data class PluginConsent(
    val id: String,
    val name: String,
    val website: String,
    val resources: List<PluginConsentResource>,
    val actions: List<PluginConsentAction>,
    /** It replaces the imported plugin of the same id. */
    val replaces: Boolean = false,
) {
    /** It shows at least one Secret's data in clear. */
    val revealsSecrets: Boolean get() = resources.any { it.revealed }

    /** It only reads: no action. */
    val readOnly: Boolean get() = actions.isEmpty()
}

fun PluginUserItem.consent(): PluginConsent = PluginConsent(
    id = id,
    name = name.ifEmpty { id },
    website = website,
    resources = resources.map { r ->
        val secret = r.group.isEmpty() && r.resource == "secrets"
        PluginConsentResource(
            name = if (r.group.isEmpty()) r.resource else "${r.resource}.${r.group}",
            kind = r.kind,
            secret = secret,
            revealed = secret && r.reveal,
        )
    },
    actions = actions.map { a ->
        PluginConsentAction(
            label = a.label.ifEmpty { a.id },
            verb = a.verb,
            needs = listOf(a.rbac.joinToString(", "), a.rbacResource).filter { it.isNotEmpty() }.joinToString(" "),
        )
    },
    replaces = replaces,
)

/** At most what an imported plugin file may be: the core refuses more. */
const val PLUGIN_MAX_BYTES = 1 shl 20
