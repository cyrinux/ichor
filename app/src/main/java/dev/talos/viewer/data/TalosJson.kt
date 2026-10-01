package dev.talos.viewer.data

import kotlinx.serialization.json.Json

/** Lenient decoder: unknown fields from newer Go cores are ignored. */
val TalosJson = Json {
    ignoreUnknownKeys = true
    explicitNulls = false
    // Go marshals nil slices as `null` (e.g. a context without `nodes`): fall back to defaults.
    coerceInputValues = true
}
