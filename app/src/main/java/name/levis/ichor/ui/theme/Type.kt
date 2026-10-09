package name.levis.ichor.ui.theme

import androidx.compose.material3.Typography
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily

/**
 * Dense monospace text (log lines, packets, hex dumps): the theme's smallest body size, so it
 * never drops below 12sp and follows the user's font scale like the rest of the app.
 */
val Typography.monoSmall: TextStyle
    get() = bodySmall.copy(fontFamily = FontFamily.Monospace)
