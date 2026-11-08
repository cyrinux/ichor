package name.levis.ichor.model

import androidx.annotation.StringRes
import name.levis.ichor.R

/** A pull problem's one-line title: [res], with the registry as its argument when [takesRegistry]. */
data class PullTitle(@StringRes val res: Int, val takesRegistry: Boolean)

/** The pod row's line for this kind of pull problem. */
fun PullKind.title(): PullTitle = when (this) {
    PullKind.NOT_FOUND -> PullTitle(R.string.pod_pull_notFound, true)
    PullKind.UNAUTHORIZED -> PullTitle(R.string.pod_pull_unauthorized, true)
    PullKind.RATE_LIMITED -> PullTitle(R.string.pod_pull_rateLimited, true)
    PullKind.TLS -> PullTitle(R.string.pod_pull_tls, true)
    PullKind.NETWORK -> PullTitle(R.string.pod_pull_network, true)
    PullKind.PLATFORM -> PullTitle(R.string.pod_pull_platform, false)
    PullKind.INVALID_NAME -> PullTitle(R.string.pod_pull_invalidName, false)
    PullKind.UNKNOWN -> PullTitle(R.string.pod_pull_unknown, true)
}

/**
 * What to do about it. [hasSecrets]: the pod names pull secrets (the unauthorized hint lists
 * them as its argument, or says it has none).
 */
@StringRes
fun PullKind.hint(hasSecrets: Boolean): Int = when (this) {
    PullKind.NOT_FOUND -> R.string.pod_pull_hint_notFound
    PullKind.UNAUTHORIZED -> if (hasSecrets) R.string.pod_pull_hint_unauthorized else R.string.pod_pull_hint_unauthorized_none
    PullKind.RATE_LIMITED -> R.string.pod_pull_hint_rateLimited
    PullKind.TLS -> R.string.pod_pull_hint_tls
    PullKind.NETWORK -> R.string.pod_pull_hint_network
    PullKind.PLATFORM -> R.string.pod_pull_hint_platform
    PullKind.INVALID_NAME -> R.string.pod_pull_hint_invalidName
    PullKind.UNKNOWN -> R.string.pod_pull_hint_unknown
}

/** The hint of this problem: its kind's, listing the pull secrets when it has some. */
@StringRes
fun PullProblem.hint(): Int = pullKind.hint(secrets.isNotEmpty())

/** The hint's argument, null when it takes none: the pull secrets of an unauthorized pull. */
fun PullProblem.hintArg(): String? = secrets.joinToString(", ").takeIf { pullKind == PullKind.UNAUTHORIZED && secrets.isNotEmpty() }

/** The checkup's fix for a podImagePull finding whose reason is [reason] (the kind); null keeps the general one. */
@StringRes
fun pullCheckupFix(reason: String): Int? = when (PullKind.parse(reason)) {
    PullKind.NOT_FOUND -> R.string.checkup_podImagePull_fix_notFound
    PullKind.UNAUTHORIZED -> R.string.checkup_podImagePull_fix_unauthorized
    PullKind.RATE_LIMITED -> R.string.checkup_podImagePull_fix_rateLimited
    PullKind.TLS -> R.string.checkup_podImagePull_fix_tls
    PullKind.NETWORK -> R.string.checkup_podImagePull_fix_network
    PullKind.PLATFORM -> R.string.checkup_podImagePull_fix_platform
    PullKind.INVALID_NAME -> R.string.checkup_podImagePull_fix_invalidName
    PullKind.UNKNOWN -> null
}
