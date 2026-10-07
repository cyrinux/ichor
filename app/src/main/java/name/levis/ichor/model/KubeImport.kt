package name.levis.ichor.model

import androidx.annotation.StringRes
import name.levis.ichor.R

// The preview of a kubeconfig import: one row per context (ParseKubeconfig's order), the ones
// the app can add checked, the others saying why not.

/** A context of an imported kubeconfig, as its preview row shows it. */
data class KubeImportRow(
    val index: Int,
    val context: ContextSummary,
    /** No problem: it can be added (checked by default). */
    val importable: Boolean,
    val included: Boolean,
    val conflict: ImportConflict?,
    val choice: ImportChoice,
)

/** One choice per context, by position: those that cannot be added are skipped. */
fun initialKubeChoices(summary: ConfigSummary): List<ImportChoice> =
    summary.contexts.mapIndexed { index, context -> ImportChoice(index, skip = context.problem.isNotEmpty()) }

/** The preview rows of [summary] with what the user picked ([choices]) and the [conflicts] with stored clusters. */
fun kubeImportRows(summary: ConfigSummary, conflicts: List<ImportConflict>, choices: List<ImportChoice>): List<KubeImportRow> =
    summary.contexts.mapIndexed { index, context ->
        val importable = context.problem.isEmpty()
        val choice = choices.firstOrNull { it.index == index } ?: ImportChoice(index, skip = !importable)
        KubeImportRow(
            index = index,
            context = context,
            importable = importable,
            included = importable && !choice.skip,
            // A context left out clashes with nothing.
            conflict = conflicts.firstOrNull { it.index == index }?.takeIf { importable && !choice.skip },
            choice = choice,
        )
    }

/** How a kubeconfig context signs in, for the preview; null for a method the app does not know. */
@StringRes
fun kubeAuthLabel(auth: String): Int? = when (auth) {
    "cert" -> R.string.kube_auth_cert
    "token" -> R.string.kube_auth_token
    "eks" -> R.string.kube_auth_eks
    "gke" -> R.string.kube_auth_gke
    "oidc" -> R.string.kube_auth_oidc
    "azure" -> R.string.kube_auth_azure
    "digitalocean" -> R.string.kube_auth_digitalocean
    "rancher" -> R.string.kube_auth_rancher
    "exec" -> R.string.kube_auth_exec
    "auth-provider" -> R.string.kube_auth_provider
    "basic" -> R.string.kube_auth_basic
    "none" -> R.string.kube_auth_none
    else -> null
}

/** Why a kubeconfig context cannot be added (a ParseKubeconfig problem code), for the preview. */
@StringRes
fun kubeProblemText(problem: String): Int = when (problem) {
    "kube-cluster-missing" -> R.string.kube_problem_cluster_missing
    "kube-user-missing" -> R.string.kube_problem_user_missing
    "kube-not-https" -> R.string.kube_problem_not_https
    "kube-file-path" -> R.string.kube_problem_file_path
    "kube-proxy-url" -> R.string.kube_problem_proxy
    "kube-basic-auth" -> R.string.kube_problem_basic_auth
    "kube-sign-in-later" -> R.string.kube_problem_sign_in_later
    "kube-exec-unsupported" -> R.string.kube_problem_exec
    "kube-no-credentials" -> R.string.kube_problem_no_credentials
    else -> R.string.kube_problem_invalid
}
