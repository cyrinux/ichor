package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/integrations.go.

/** The projects the app integrates with; [checked] once a cluster was asked which it runs. */
@Serializable
data class Integrations(
    val checked: Boolean = false,
    val items: List<Integration> = emptyList(),
)

@Serializable
data class Integration(
    /** Catalog app id. */
    val id: String,
    /** A proper noun: never translated. */
    val name: String,
    /** Bundled icon: assets/appicons/<icon>.webp; "" when none. */
    val icon: String = "",
    val website: String = "",
    /** The API groups the app reads; empty when found by its pods (Garage). */
    val groups: List<String> = emptyList(),
    val detected: Boolean = false,
    /** API version the cluster serves for the first group; "" when unknown. */
    val version: String = "",
)

/** For [name.levis.ichor.ui.apps.AppIconTile]: the catalog app the integration is. */
val Integration.asApp: InventoryApp get() = InventoryApp(id = id, name = name, icon = icon, known = true)

/** The inventory's catalog ids, for KubeIntegrations: what has no API of its own is found by them. */
fun Inventory.integrationHints(): String = apps.filter { it.known }.joinToString(",") { it.id }
