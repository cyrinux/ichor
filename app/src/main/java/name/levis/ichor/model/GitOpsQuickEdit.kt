package name.levis.ichor.model

import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put

// Mirrors go/ichorgo/gitops_quickedit.go.

/**
 * A container of a [QuickEditDocument]: its [name] and the [image] the file sets; [overriddenBy]
 * names the kustomization `images:` entry of the same file that replaces it (edit that one).
 */
@Serializable
data class QuickEditContainer(
    val name: String = "",
    val image: String = "",
    val init: Boolean = false,
    val overriddenBy: String = "",
)

/** One workload of a file (Deployment, StatefulSet, DaemonSet, Job, CronJob), by its [index] among the file's documents. */
@Serializable
data class QuickEditDocument(
    val index: Int = 0,
    val kind: String = "",
    val name: String = "",
    /** null when the file does not set it (Kubernetes then runs 1). */
    val replicas: Int? = null,
    /** Whether the replicas op applies (not a DaemonSet, a Job or a CronJob). */
    val scalable: Boolean = false,
    val containers: List<QuickEditContainer> = emptyList(),
)

/** An entry of a kustomization's `images:` (document [documentIndex]): the image [name] it rewrites to [newName]:[newTag]. */
@Serializable
data class QuickEditKustomizeImage(
    val documentIndex: Int = 0,
    val name: String = "",
    val newName: String = "",
    val newTag: String = "",
    /** Pinned by digest: left to the editor. */
    val digest: String = "",
) {
    /** The image it makes: [newName] (else [name]) at [newTag]; [name] alone without a tag. */
    val image: String get() = newName.ifEmpty { name }.let { if (newTag.isEmpty()) it else "$it:$newTag" }
}

/** An entry of a kustomization's `replicas:` (document [documentIndex]): the workload [name] and its [count]. */
@Serializable
data class QuickEditKustomizeReplica(val documentIndex: Int = 0, val name: String = "", val count: Int? = null)

/** `GitOpsQuickEditTargets`' answer: what a file offers to quick edits. */
@Serializable
data class QuickEditTargets(
    val documents: List<QuickEditDocument> = emptyList(),
    val kustomizeImages: List<QuickEditKustomizeImage> = emptyList(),
    val kustomizeReplicas: List<QuickEditKustomizeReplica> = emptyList(),
) {
    /** The documents with containers. */
    val withContainers: List<QuickEditDocument> get() = documents.filter { it.containers.isNotEmpty() }

    val canEditImage: Boolean get() = quickImageChoices(this).isNotEmpty()
    val canEditReplicas: Boolean get() = quickReplicaChoices(this).isNotEmpty()
    val isEmpty: Boolean get() = !canEditImage && !canEditReplicas
}

/**
 * A quick edit of a file, `GitOpsQuickEdit`'s editJSON: [OP_IMAGE] sets [image] on [container]
 * of document [documentIndex] (or, with [kustomizeImage], the tag of that entry of a
 * kustomization's `images:`); [OP_REPLICAS] sets [replicas] on document [documentIndex] (or,
 * with [kustomizeReplica], on that entry of its `replicas:`).
 */
@Serializable
data class QuickEdit(
    val op: String = "",
    val documentIndex: Int = 0,
    val container: String = "",
    val image: String = "",
    val replicas: Int = 0,
    val kustomizeImage: String = "",
    val kustomizeReplica: String = "",
    /** What the pull request's title names (not sent to the core). */
    val name: String = "",
    /**
     * The kind and metadata.name of the workload document a container or workload replicas
     * edit targets: the core checks [documentIndex] still holds it (else finds it by them).
     */
    val targetKind: String = "",
    val targetName: String = "",
) {
    companion object {
        const val OP_IMAGE = "image"
        const val OP_REPLICAS = "replicas"

        /** [container] of the workload [kind]/[workload] (document [documentIndex]) at [image]. */
        fun image(documentIndex: Int, container: String, image: String, kind: String = "", workload: String = "") =
            QuickEdit(OP_IMAGE, documentIndex, container = container, image = image, targetKind = kind, targetName = workload)

        /** The kustomization entry [entry] of document [documentIndex] at [tag] (a bare tag: the core sets `newTag`). */
        fun kustomize(documentIndex: Int, entry: String, tag: String) =
            QuickEdit(OP_IMAGE, documentIndex, image = tag, kustomizeImage = entry, name = imageRepository(entry).substringAfterLast('/'))

        /** The workload of [kind] named [name] scaled to [replicas]; with [kustomizeReplica], that kustomization entry instead. */
        fun replicas(documentIndex: Int, name: String, replicas: Int, kustomizeReplica: String = "", kind: String = "") =
            QuickEdit(
                OP_REPLICAS, documentIndex, replicas = replicas, kustomizeReplica = kustomizeReplica, name = name,
                targetKind = if (kustomizeReplica.isEmpty()) kind else "",
                targetName = if (kustomizeReplica.isEmpty() && kind.isNotEmpty()) name else "",
            )
    }
}

/** `GitOpsQuickEdit`'s answer: the rewritten [content], whether it [changed], the 1-based [line] edited. */
@Serializable
data class QuickEditResult(val content: String = "", val changed: Boolean = false, val line: Int = 0)

private val QuickJson = Json {
    ignoreUnknownKeys = true
    explicitNulls = false
    coerceInputValues = true
}

/**
 * `GitOpsQuickEdit`'s editJSON for [edit]: only the keys of its op (the core refuses unknown
 * ones), plus the target workload's kind and name for a container or workload replicas edit.
 */
fun quickEditJson(edit: QuickEdit): String = buildJsonObject {
    put("op", edit.op)
    put("documentIndex", edit.documentIndex)
    if (edit.kustomizeImage.isEmpty() && edit.kustomizeReplica.isEmpty()) {
        if (edit.targetKind.isNotEmpty()) put("kind", edit.targetKind)
        if (edit.targetName.isNotEmpty()) put("name", edit.targetName)
    }
    when {
        edit.op == QuickEdit.OP_REPLICAS -> {
            if (edit.kustomizeReplica.isNotEmpty()) put("kustomizeReplica", edit.kustomizeReplica)
            put("replicas", edit.replicas)
        }
        edit.kustomizeImage.isNotEmpty() -> {
            put("kustomizeImage", edit.kustomizeImage)
            put("image", edit.image)
        }
        else -> {
            put("container", edit.container)
            put("image", edit.image)
        }
    }
}.toString()

/** [edit] for a route argument, every key kept (the title needs [QuickEdit.name]). */
fun encodeQuickEdit(edit: QuickEdit): String = QuickJson.encodeToString(QuickEdit.serializer(), edit)

/** A route's quick edit back; null when absent or not one. */
fun decodeQuickEdit(json: String?): QuickEdit? {
    if (json.isNullOrBlank()) return null
    return runCatching { QuickJson.decodeFromString(QuickEdit.serializer(), json) }.getOrNull()
        ?.takeIf { it.op == QuickEdit.OP_IMAGE || it.op == QuickEdit.OP_REPLICAS }
}

fun decodeQuickEditTargets(json: String): QuickEditTargets = QuickJson.decodeFromString(QuickEditTargets.serializer(), json)

fun decodeQuickEditResult(json: String): QuickEditResult = QuickJson.decodeFromString(QuickEditResult.serializer(), json)

/**
 * The pull request's default title for [edit] (English: it is text of the repository, not of
 * the app): "Bump podinfo to 6.7.0", "Scale podinfo to 3".
 */
fun quickEditTitle(edit: QuickEdit): String = when (edit.op) {
    QuickEdit.OP_REPLICAS -> "Scale ${edit.name.ifEmpty { "workload" }} to ${edit.replicas}"
    else -> {
        val what = edit.container.ifEmpty { edit.name }.ifEmpty { imageRepository(edit.image).substringAfterLast('/') }
        "Bump $what to ${imageTag(edit.image).ifEmpty { edit.image }}"
    }
}

/**
 * The pull request prefilled for [draft]: titled (and its commit message) after a quick edit
 * ([quickTitle]) while the draft is still exactly what it made ([quickDraft]), else [defaults].
 */
fun quickProposalDefaults(defaults: GitProposalDefaults, quickDraft: String?, quickTitle: String?, draft: String): GitProposalDefaults =
    if (quickDraft != null && !quickTitle.isNullOrBlank() && draft == quickDraft) defaults.copy(title = quickTitle, message = quickTitle) else defaults

/**
 * The repository of [image], without its tag or digest: "ghcr.io/org/app:1.2@sha256:…" gives
 * "ghcr.io/org/app"; a registry port ("host:5000/app") is kept.
 */
fun imageRepository(image: String): String {
    val noDigest = image.substringBefore('@')
    val colon = noDigest.lastIndexOf(':')
    return if (colon > noDigest.lastIndexOf('/')) noDigest.substring(0, colon) else noDigest
}

/** The tag of [image], "" when it has none (a digest alone is not a tag). */
fun imageTag(image: String): String {
    val noDigest = image.substringBefore('@')
    val colon = noDigest.lastIndexOf(':')
    return if (colon > noDigest.lastIndexOf('/')) noDigest.substring(colon + 1) else ""
}

/** [image] at [tag], its old tag and digest dropped. */
fun imageWithTag(image: String, tag: String): String {
    val t = tag.trim()
    val repo = imageRepository(image)
    return if (t.isEmpty()) repo else "$repo:$t"
}

/** Whether [tag] is an image tag (a digest is changed in the editor): no spaces, slashes or colons. */
fun isValidImageTag(tag: String): Boolean = TAG.matches(tag.trim())

private val TAG = Regex("^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$")

/** The tag of the first of [images] from the repository of [image] whose tag is not [image]'s; null when none. */
fun runningTag(image: String, images: List<String>): String? {
    val repo = imageRepository(image)
    val current = imageTag(image)
    return images.firstOrNull { imageRepository(it) == repo && imageTag(it) != current && imageTag(it).isNotEmpty() }?.let(::imageTag)
}

/**
 * The tag the workload ran before: from [revisions] (a Deployment's kept revisions, newest
 * first, each with its images), the newest one whose image of [image]'s repository has a tag
 * not in [exclude] (the file's and the running one); null when none.
 */
fun previousTag(image: String, revisions: List<List<String>>, exclude: Set<String>): String? {
    val repo = imageRepository(image)
    return revisions.asSequence()
        .mapNotNull { images -> images.firstOrNull { imageRepository(it) == repo } }
        .map(::imageTag)
        .firstOrNull { it.isNotEmpty() && it !in exclude }
}

/**
 * One image the image sheet can change: [container] of document [documentIndex], or the
 * kustomization `images:` entry [kustomizeImage] (it wins over the manifests' `image:`).
 */
data class QuickImageChoice(
    val label: String,
    val image: String,
    val documentIndex: Int = 0,
    val container: String = "",
    val kustomizeImage: String = "",
    /** The container's workload document: its kind and metadata.name. */
    val kind: String = "",
    val workload: String = "",
) {
    val isKustomize: Boolean get() = kustomizeImage.isNotEmpty()

    /** The edit that sets [tag] on it. */
    fun edit(tag: String): QuickEdit =
        if (isKustomize) {
            QuickEdit.kustomize(documentIndex, kustomizeImage, tag.trim())
        } else {
            QuickEdit.image(documentIndex, container, imageWithTag(image, tag), kind, workload)
        }
}

/**
 * The images of [targets] to choose from: the kustomization's entries first (the GitOps place
 * of a tag; one pinned by digest is left to the editor), then each container no entry
 * overrides; a container is labelled with its workload when the file has several.
 */
fun quickImageChoices(targets: QuickEditTargets): List<QuickImageChoice> {
    val docs = targets.withContainers
    val kustomize = targets.kustomizeImages.filter { it.name.isNotEmpty() && it.digest.isEmpty() }
        .map { QuickImageChoice(label = it.name, image = it.image, documentIndex = it.documentIndex, kustomizeImage = it.name) }
    val containers = docs.flatMap { doc ->
        doc.containers.filter { it.name.isNotEmpty() && it.overriddenBy.isEmpty() }.map { c ->
            QuickImageChoice(
                label = if (docs.size > 1) "${doc.name}/${c.name}" else c.name,
                image = c.image,
                documentIndex = doc.index,
                container = c.name,
                kind = doc.kind,
                workload = doc.name,
            )
        }
    }
    return kustomize + containers
}

/** One replica count the replicas sheet can set: a workload, or a kustomization `replicas:` entry ([kustomizeReplica]). */
data class QuickReplicaChoice(
    val label: String,
    val name: String,
    val current: Int,
    val documentIndex: Int,
    val kustomizeReplica: String = "",
    /** The workload's kind ("" for a kustomization entry). */
    val kind: String = "",
) {
    fun edit(replicas: Int): QuickEdit = QuickEdit.replicas(documentIndex, name, replicas, kustomizeReplica, kind)
}

/** The replica counts of [targets]: each scalable workload (1 when unset), then each kustomization `replicas:` entry. */
fun quickReplicaChoices(targets: QuickEditTargets): List<QuickReplicaChoice> {
    val workloads = targets.documents.filter { it.scalable }
        .map { QuickReplicaChoice("${it.kind}/${it.name}", it.name, it.replicas ?: 1, it.index, kind = it.kind) }
    val kustomize = targets.kustomizeReplicas.filter { it.name.isNotEmpty() }
        .map { QuickReplicaChoice(it.name, it.name, it.count ?: 1, it.documentIndex, kustomizeReplica = it.name) }
    return workloads + kustomize
}

/** The tags the image sheet offers as chips: the one the workload ran before, the one it runs now. */
data class QuickTagChips(val previous: String? = null, val running: String? = null)

/**
 * The chips for [image] (as the file sets it): [QuickTagChips.running] from the Deployment's
 * current revision, else the app's [liveImages], when its tag differs from the file's;
 * [QuickTagChips.previous] from the Deployment's older [revisions] (newest first), a tag
 * neither the file nor the cluster runs.
 */
fun quickTagChips(image: String, liveImages: List<String>, revisions: List<KubeRevision>): QuickTagChips {
    val current = revisions.filter { it.current }.flatMap { it.images }
    val running = runningTag(image, current + liveImages)
    val runningNow = current.firstOrNull { imageRepository(it) == imageRepository(image) }?.let(::imageTag)
    val exclude = setOfNotNull(imageTag(image), running, runningNow)
    val previous = previousTag(image, revisions.filterNot { it.current }.map { it.images }, exclude)
    return QuickTagChips(previous = previous, running = running)
}
