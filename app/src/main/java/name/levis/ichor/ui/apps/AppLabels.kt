package name.levis.ichor.ui.apps

import androidx.compose.runtime.Composable
import androidx.compose.ui.res.stringResource
import name.levis.ichor.R

/** The localized label of a catalog category (unknown ones read as "Other"). */
@Composable
fun categoryLabel(category: String): String = stringResource(
    when (category) {
        "system" -> R.string.apps_category_system
        "networking" -> R.string.apps_category_networking
        "storage" -> R.string.apps_category_storage
        "observability" -> R.string.apps_category_observability
        "security" -> R.string.apps_category_security
        "database" -> R.string.apps_category_database
        "messaging" -> R.string.apps_category_messaging
        "devops" -> R.string.apps_category_devops
        "media" -> R.string.apps_category_media
        "home" -> R.string.apps_category_home
        "productivity" -> R.string.apps_category_productivity
        "ai" -> R.string.apps_category_ai
        "web" -> R.string.apps_category_web
        else -> R.string.apps_category_other
    },
)
