package name.levis.ichor.ui.components

import name.levis.ichor.ui.node.NodeMenuEntry

// The anchors the demo-mode smoke test (androidTest DemoSmokeTest) waits for and taps. Test
// tags are not shown or read out; when a screen moves an anchor, move its tag, not the test.

/** An overview node row (or chip). */
const val SMOKE_NODE = "smoke-node"

/** The demo tour's Skip: its coach marks cover the overview on the first demo open. */
const val SMOKE_TOUR_SKIP = "smoke-tour-skip"

/** The node screen's list (its header and menu). */
const val SMOKE_NODE_SCREEN = "smoke-node-screen"

/** The log lines of a logs screen. */
const val SMOKE_LOG_LINES = "smoke-log-lines"

/** The Kubernetes home of a cluster added from a kubeconfig. */
const val SMOKE_KUBE_HOME = "smoke-kube-home"

/** The title of the overview's bar: tapping it opens the cluster menu. */
const val SMOKE_CLUSTER_TITLE = "smoke-cluster-title"

/** The cluster menu's Upgrade cluster entry. */
const val SMOKE_CLUSTER_UPGRADE = "smoke-cluster-upgrade"

/** The packet capture setup's Start button. */
const val SMOKE_CAPTURE_START = "smoke-capture-start"

/** A node's services, once loaded (the screenshot walks, androidTest DemoWalk). */
const val SMOKE_NODE_SERVICES = "smoke-node-services"

/** A node's machine events, once one has arrived. */
const val SMOKE_EVENTS = "smoke-events"

/** A node's live graphs, once they hold a sample. */
const val SMOKE_GRAPHS = "smoke-graphs"

/** A running capture, once it has shown a packet. */
const val SMOKE_CAPTURE = "smoke-capture"

/** The cluster upgrade screen, once its plan is loaded. */
const val SMOKE_UPGRADE = "smoke-upgrade"

/** A node screen's menu entry. */
fun smokeMenuTag(entry: NodeMenuEntry) = "smoke-node-menu-${entry.name}"

/** A container row of a node's Containers tab (it opens the container's log). */
const val SMOKE_CONTAINER_ROW = "smoke-container-row"

/** An app bar's ⋮ button, the actions that are not icons behind it. */
const val SMOKE_ACTIONS_MORE = "smoke-actions-more"

/** The overview's Settings action: its icon, or its item in the ⋮ menu. */
const val SMOKE_SETTINGS = "smoke-settings"

/** The Settings screen's list of pages. */
const val SMOKE_SETTINGS_SCREEN = "smoke-settings-screen"
