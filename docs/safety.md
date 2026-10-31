# Safety features

Part of the [Ichor documentation](../README.md#documentation).


- **Reboot / shutdown:** the same options as talosctl, from the Talos v1.14 sequencer:

  | Option | What happens |
  |---|---|
  | Reboot `default` | Stop pods and services, then reboot (kexec fast reboot when available). No cordon/drain. |
  | Reboot `powercycle` | Same graceful stop, then a full firmware reboot instead of kexec. |
  | Reboot `force` | Reboot immediately, with no pod or service shutdown. Only for a stuck node. |
  | Shutdown `--force` | Skip the Kubernetes cordon/drain, e.g. when the API is down. Pods and services are still stopped. |

  Every option goes through the same confirmation:
  1. You type the node's hostname to confirm.
  2. Control-plane nodes get an etcd-quorum warning.
  3. With the app lock on, you also authenticate with fingerprint or PIN, or a security key.
- **Reset (node menu → Reset…):** `talosctl reset` with a plan first. The last control plane,
  or one whose departure would cost etcd its quorum, cannot be reset: the app and the Go core
  both refuse it. The options, then the same typed hostname and app lock as a reboot:

  | Option | What happens |
  |---|---|
  | Wipe everything | The system disk and the node's other disks, listed in the sheet. |
  | Wipe the system disk | The system disk only: the node starts over as a new machine. |
  | Wipe the user disks | The node's other disks only. |
  | Leave etcd first (`--graceful`, on by default) | Cordon, drain and leave etcd cleanly first. Off on a control plane, its etcd member stays behind: remove it from the etcd screen. |
  | Reboot after (`--reboot`, on by default) | Off, the node stays powered off once wiped. |

- **Replacing a control plane (etcd → a failed member's menu → Replace this control plane…,
  `os:admin`):** one screen walks through the replacement, step by step, read from the cluster
  so that leaving and coming back resumes:
  1. **Check etcd quorum:** the removal must leave enough healthy members. A healthy member is
     never offered, and a removal that would lose quorum is blocked with the reason.
  2. **Remove the etcd member:** the usual removal plan and typed hostname. The Go core reads
     the plan again first, refuses a member that recovered, and sends the removal through a
     healthy control plane.
  3. **Reset the old node:** the reset sheet, without `--graceful` (the member already left).
     A node that no longer answers is skipped: power it off yourself. So is an address that a
     current member now uses (a replacement that took the old IP).
  4. **Boot the new node:** by hand for now. Boot it from a Talos image, open the machine config
     of a healthy control plane (secrets revealed only after the app lock), save it as
     `controlplane.yaml`, then run `talosctl apply-config --insecure -n <new-node-ip> -f controlplane.yaml`
     from a laptop.
  5. **Wait for the new member:** the screen polls etcd until a new healthy voting member joins.

- **Making a Talos snapshot at Hetzner (Create the server at Hetzner → Make a Talos snapshot):**
  billable, so the cost shows first, and nothing of the cluster is touched. The run creates, with a
  Read & Write token: one temporary SSH key, one small server in the rescue system (its host key
  pinned for the run, since it cannot be known in advance), then the snapshot, labelled `os=talos`.
  It deletes the server and the key on every path, and an unfinished snapshot when it fails or is
  stopped; the message names what was deleted. The one case that needs the Hetzner console is a
  cleanup that fails: the message names the server and the key to delete there, and they stay
  billed until then.
- **Rotating the Talos CA (Settings → Clusters & backup → Rotate the cluster CA…, `os:admin`):**
  replaces the CA that signs every Talos API certificate, in the order of
  `talosctl rotate-ca --kubernetes=false`. Rotate it when a talosconfig leaked or a phone was
  lost: a client certificate cannot be revoked any other way.
  - **Plan first:** the current CA (subject, SHA-256, expiry) and every node the rotation patches:
    each member cluster discovery lists, else the nodes the talosconfig names. A node that does
    not answer, or runs Talos before 1.7 (no `machine.acceptedCAs`), blocks the rotation, since a
    node left out would stop trusting the phone. Omni clusters are refused: Omni holds their CA.
  - **Confirmation:** the context name, typed, and with the app lock on a fresh fingerprint/PIN.
  - **The run, five steps, control planes first, every node checked after each one:**

    | Step | What happens |
    |---|---|
    | Check every node | The plan is read again; every node must answer with the current certificate. |
    | Accept the new CA | The new CA joins each node's accepted CAs (no reboot); every node must then answer a certificate of the new CA. |
    | Store the new talosconfig | The phone stores a transition talosconfig (a new `os:admin` certificate, both CAs trusted) and tells the run it did. Nothing else changes until then. |
    | Issue with the new CA | The new CA becomes the issuing one; the old one stays accepted. Every node must answer with the new CA alone. |
    | Drop the old CA | The old CA leaves each node's accepted CAs; every node is checked once more, then the final talosconfig (the new CA only) is stored. |

  - **Never locked out:** the phone's new talosconfig is stored before any node stops issuing
    with the old CA, and the transition one trusts both. A run stopped at any step (Stop, a
    failed check, the app killed) leaves the cluster with CAs that this phone's stored
    talosconfig still reaches. A transition talosconfig that cannot be stored stops the run
    before the issuing CA changes. A run stopped while the new CA becomes the issuing one can
    leave the control planes on different CAs: the plan then says so and blocks, and
    `talosctl rotate-ca` finishes it from a computer.
  - **The cluster stays the same cluster:** its pins, colour, name, history and share links are
    kept under the fingerprint it had before the rotation. Backups carry that link, so a restore
    on another phone keeps them too.
  - **Afterwards:** every other talosconfig (other devices, `talosctl` on a computer) stops working:
    issue new ones from Issue talosconfig. Configs generated later from the old secrets bundle
    (`talosctl gen config`) carry the old CA. The Kubernetes CA is not rotated: kubeconfig
    identities keep working, and `talosctl rotate-ca --talos=false` rotates it from a computer.
  - **In the background:** on Android the rotation goes on in a notification with Stop. On iOS it
    runs while iOS allows; once suspended it waits, and the cluster keeps the CAs of that step.
  - **Activity log:** recorded as `talos-ca-rotate` with the node count and the step reached, never a key.

- **Container restart (node → Pods → a container's menu → Restart container…, `os:admin`):**
  `talosctl restart` without a reboot. A Talos system container (apid, trustd, an extension
  service) asks for the typed hostname; a Kubernetes container asks a plain confirm, as the
  kubelet starts it again. With the app lock on, you authenticate first.

- **Policy mode (Kubernetes → Policies → a Kyverno policy, `os:admin`):** switching a policy
  to Audit, which lets requests that break it through, asks for the typed policy name; switching
  back to Enforce asks a plain confirm. The button is off, with the reason, when the credentials
  may not patch the policy, and the activity log keeps every switch.

- **Autoscaling (Kubernetes → Autoscaling → a ScaledObject or an HPA, `os:admin`):** pausing,
  holding or resuming a KEDA ScaledObject and editing an HPA's min and max are reversible: each
  asks a plain confirm, except a pause at 0, which stops every pod and so asks for the
  ScaledObject's name typed, like a scale to 0. Each button is off, with the reason, when the credentials may not patch
  that kind; the activity log keeps every change; the demo cluster refuses them.

- **META keys (node menu → Machine config → META, `os:admin`):**
  - **What they are:** the boot-time values Talos reads before the machine config, such as the
    network config of a metal node (0x0a). This is the last-resort fix for a node that cannot boot
    with its config.
  - **Writing or deleting a key:** asks for the typed hostname, like `talosctl meta write` and
    `talosctl meta delete`. With the app lock on, you authenticate first.
  - **What the app refuses or hides:** keys that only Talos writes (upgrade, STATE encryption)
    cannot be changed. Secret values are never shown. The activity log keeps the key and the
    size, never the value.

- **Machine config changes (node menu → Machine config → Edit):**
  - **Review first:** the node checks every change with a dry run before anything is applied.
  - **Snippets:** while editing, ready-made changes cover the usual ones: NTP and DNS servers, an
    interface MTU, a sysctl, kernel arguments, a kubelet or API server argument, a node label, a
    registry mirror, KubeSpan on or off. You fill a few fields, the change lands in the draft and
    goes through the same review. On Talos 1.12+ configs, a setting held in its own document
    (ResolverConfig, TimeSyncConfig, KubeletConfig…) is changed there.
  - **Ways to apply it:**

    | Mode | What happens |
    |---|---|
    | Try for 1, 5 or 10 min | Applied without a reboot; the node goes back to its previous config by itself unless you keep it. |
    | Apply now | Applied for good, without a reboot. Refused when the change needs one. |
    | Apply at the next reboot | Saved on the node (`--mode staged`), applied when it next reboots. |
    | Apply and reboot now | Applied, then the node reboots; you type its hostname first. |

  - **Changes that need a reboot:** they offer only the last two modes.
  - **A try keeps running in the background:** leave the screen and the countdown goes on in a notification, with **Keep** and **Revert now**.
    - **App lock:** with it on, Keep opens the app first.
    - **On iOS:** the background time is short. Once iOS suspends Ichor, the node reverts by itself at the deadline, and the notification says so.
  - **The same change on several nodes ("Also apply to other nodes…"):**
    - **Preview:** the field edits are replayed on each picked node's own config, and you see each node's diff first. A node the edits do not fit is skipped.
    - **Run:** one confirmation for all, typed with the cluster's name. Nodes are applied one after the other, workers first and control planes last, and the first failure stops the rest.
    - **Reboot mode:** each node is back before the next starts, and etcd must be healthy before a control plane reboots.
    - **Try mode:** for one node only.
    - **In the background:** on Android the rollout goes on in a notification, node by node, after you leave the app. On iOS it runs while iOS allows, and a notification asks you to come back.
- **App lock (Settings → Security):**
  - **Methods:** fingerprint, with the device PIN, pattern or password as fallback; or a
    **security key** (below).
  - **When it asks:** at launch, after 30 s in the background, and before reboot, shutdown and kubeconfig export.
  - **Privacy:** it hides the app in recent apps and blocks screenshots, unless you turn on
    "Allow screenshots", which also requires authenticating.
  - **Changing it:** turning it on or off requires authenticating.
- **Security keys (Settings → Security → Security keys):** a YubiKey or any FIDO2 key, tapped on
  the back of the phone (NFC) or plugged in (USB), opens the app and confirms the risky actions
  instead of the fingerprint. Up to two keys (one worn, a spare). Ichor makes a FIDO2 credential
  on the key (user presence only: a touch, never the key's PIN) and checks its signature over a
  fresh challenge each time; it talks to the key itself with Yubico's SDK, no Play Services and no
  account. The PIN that the app lock falls back to is the **device** PIN: a forgotten device PIN
  locks the phone itself, and no app can offer a key as a way around that.
  - **Require the key:** the stored configs (talosconfig, kubeconfig, the kubeconfig sign-ins) are
    sealed a second time with a data key that only the hmac-secret output of one of the enrolled
    keys unwraps, on top of the Keystore encryption. Fingerprint or PIN alone no longer opens the
    app, and a cold start reads nothing until a key is tapped. The data key is held in memory
    while the app process lives, so **background alerts and the widget pause once Android has
    closed the app, until you unlock it again**. Losing every enrolled key means deleting the
    stored configs and importing them again ("I lost my security keys" on the lock screen).
  - **Adding a key** needs a tap (and, once, the key's FIDO PIN if the key insists on it for new
    credentials); with the requirement on, a second tap of the same key. Turning the requirement
    on asks for a tap of every enrolled key; turning it off asks for one tap. A key set to always
    verify the user (fingerprint or PIN on the key itself) computes another secret, so changing
    that setting after enrolment needs the requirement turned off and on again.
- **Keep last known state (Settings → Privacy, on by default):** the last data fetched from each
  cluster is kept on the phone, encrypted, for 24 hours and never backed up, so the home opens on
  it at once and still shows it when the cluster cannot be reached. Turn it off to keep cluster
  data in memory only (an install that had turned it off keeps it off). With it off but background
  alerts on, the home opens on the nodes the last background check saw, labelled as such, while
  it refreshes; never in screenshot mode.
- **Notifications:** with the app lock on, alerts show only a generic text on the lock screen.
  The widget and the Wear OS tile show counts only, no hostnames.
- **Kubeconfig export:** the kubeconfig is written only to the file you choose. Use it with
  kubectl or any Kubernetes client, then delete the file.
