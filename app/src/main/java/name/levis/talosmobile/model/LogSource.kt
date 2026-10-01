package name.levis.talosmobile.model

/** What a log screen shows. */
sealed interface LogSource {
    /** Distinguishes the view models of two logs of the same node. */
    val key: String

    /** A Talos service log, or the kernel log (dmesg) when [name] is null. */
    data class Service(val name: String?) : LogSource {
        override val key: String get() = name ?: "kernel"
    }

    /** A CRI container's log; [title] is its name, [subtitle] its "namespace/pod". */
    data class Container(val id: String, val title: String, val subtitle: String) : LogSource {
        override val key: String get() = "container-$id"
    }
}

/** Title of a container's log: its name, or the short id of an unnamed one. */
fun containerLogTitle(info: ContainerInfo): String = info.name.ifEmpty { info.id.take(12) }

/** "namespace/pod", or whichever part is known. */
fun containerLogSubtitle(info: ContainerInfo): String = listOf(info.podNamespace, info.pod).filter { it.isNotEmpty() }.joinToString("/")
