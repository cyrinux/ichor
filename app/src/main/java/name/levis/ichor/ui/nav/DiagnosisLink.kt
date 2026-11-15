package name.levis.ichor.ui.nav

import androidx.compose.runtime.compositionLocalOf

/**
 * Opens the AI diagnosis with a note already written (what an Explain with AI card was about),
 * provided around the whole navigation; null where nothing can open it (a preview, a test).
 */
val LocalOpenDiagnosis = compositionLocalOf<((note: String) -> Unit)?> { null }
