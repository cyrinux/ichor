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
- **Creating an instance at OVHcloud (Add a node… → A server at a cloud provider → OVHcloud):**
  billable, so the hourly price shows first when the public catalog answers, and the host name is
  typed to confirm. The service account's secret stays sealed on the device and out of backups; it
  can do what its IAM policy allows, so give it only the policy the app prints. The run deletes only
  the one instance it created, by id, when it fails or is stopped, except a control plane that
  already answered. A create that gets no clear answer deletes nothing, because another instance may
  carry the same name: the message says to check the project, where it may stay billed.
- **Creating an Instance at Scaleway (Go core and the probe's `scaleway create`, `-yes` only; the
  app's card comes next):** billable. It makes a flexible routed IPv4, then an Instance from a
  Talos image of the project with a second volume for EPHEMERAL (local storage when the type has
  room, else Block Storage), writes the node's config as the `cloud-init` user data while the
  Instance is stopped, and powers it on; both carry the run's `ichor-run` tag. The API key's secret
  stays sealed on the device; give its application only the policy the app prints (Instances and
  Block Storage on one project). When the run fails or is stopped it deletes what it made, except
  a control plane that already answered: a running Instance is terminated (which takes its local
  volumes), one that never ran is deleted with its volumes, then a Block Storage volume and the
  flexible IP, which outlive the Instance. A create without a clear answer is looked for by the
  run's tag and deleted. The one case that needs the console is a delete that fails: the message
  names the zone and the ids still there, billed until they are deleted.
- **Creating an instance at AWS EC2 (Add a node… → A server at a cloud provider → AWS):**
  billable, so the hourly price shows first when the Price List answers, the host name is typed to
  confirm, and Stop asks once more (the instance is billed until it is terminated); the probe's
  `aws create` runs only with `-yes`. The access keys stay sealed on the device and out of backups;
  they can do what their IAM policy allows, so give their user only the policy the app prints. Before any billable call the run checks the keys
  (`GetCallerIdentity`), the AMI's architecture against the instance type, the security groups
  against the subnet's VPC, the user data against EC2's 16 KB, then sends `RunInstances` as a dry
  run with every parameter: keys that may not run it are refused, naming the policy to add. The
  run creates one instance, tagged `Name`, `ichor-run` (the run's id) and `ichor-cluster` (a hash
  of the context name, not the name), with a client token of `ichor-` and the run's id. It
  terminates only that instance, by id, when it fails or is stopped, except a control plane that
  already answered. A `RunInstances` that gets no clear answer is looked up by its client token
  and terminated. The printed policy lets the keys terminate only `ichor-run`-tagged instances,
  and tag only on `RunInstances`. The one case that needs the AWS console is a terminate that
  fails: the message names the instance id and the region, and the instance stays billed until
  it is terminated there.
- **The guided network at AWS (Use a network Ichor sets up):** not billable, but it changes the
  account, so it runs only when tapped, and the probe's `aws network` only with `-yes`. In the
  default VPC it makes one security group, `ichor-nodes`, tagged `ichor-managed`, or reuses it;
  it adds only two kinds of inbound rule: all traffic from the group itself, and, on plain Talos,
  TCP 50000 from the phone's current public IPv4 as a /32, described `Ichor phone <date>`. It never
  opens anything to `0.0.0.0/0` or `::/0`, never opens 6443, and never deletes a rule or the
  group. The phone's address changes (mobile networks, NAT): each run adds today's address and
  keeps the old ones, so remove the stale `Ichor phone` rules at AWS from time to time. A group
  named `ichor-nodes` that the app did not tag is used as it is and never changed, and the
  printed policy lets the keys add rules only to `ichor-managed` groups. Recorded in the action
  log (`aws-network-prepare`, with the region and the group id).
- **Making a Talos AMI at AWS (from the Go core and the probe for now):** billable, so the
  estimate gives the helper's hourly price and the snapshot's storage price first, and the probe's
  `aws image` runs only with `-yes`. Nothing of the cluster is touched: the schematic comes from
  the template's machine config, and a template whose config cannot be read is refused. Before any
  billable call the run checks the keys, the subnet and the security group's VPC, that no AMI of
  the account already has the name, then sends `RunInstances` as a dry run. It creates one
  throwaway Amazon Linux instance with two volumes (its root and an empty 8 GiB one), tagged
  `ichor-run`; the instance downloads the Image Factory's image itself (the phone moves no image
  bytes), writes it to the empty volume and powers off. The run reads the result on the
  instance's serial console and takes no snapshot without it, then snapshots the volume and
  registers the AMI, both tagged `os=talos`, `ichor-talos-version`, `ichor-arch` and
  `ichor-schematic`. It terminates the instance on every path (its volumes go with it), and
  deletes an unfinished snapshot or AMI when it fails or is stopped; a finished AMI and its
  snapshot stay, billed per GB-month until you deregister and delete them. The printed policy lets
  the keys stop, delete and deregister only `ichor-run`-tagged resources. The one case that needs
  the AWS console is a delete that fails: the message names the ids and the region.
- **Creating a VM at Azure (Add a node… → A server at a cloud provider → Azure):** billable, so
  the hourly price shows first when the Retail Prices API answers, the host name is typed to
  confirm, and Stop asks once more (the VM and its disk are billed until deleted); the probe's
  `azure create` runs only with `-yes`. The service principal stays sealed on the device and out
  of backups; its client secret expires on the date set when it was made and can do what its role
  allows, so give it only the custom role the app prints, on one resource group. The credential is a service principal; its token is asked
  for Resource Manager's audience only and kept for the run. Before any write the run checks the
  image's Hyper-V generation and architecture against the VM size, the subnet's virtual network
  and the security group against the location, that no VM, NIC or public IP of the host's name is
  in the resource group, the custom data against Azure's 48 KiB (64 KB once encoded), then reads
  the principal's permissions on the resource group: Azure has no dry run, so a principal whose
  roles lack a write or delete the run makes is refused there, naming the action and the role to
  paste. The run creates at most three resources, all named after the host name and tagged `Name`,
  `ichor-run` (the run's id) and `ichor-cluster` (a hash of the context name, not the name): the
  public IP `<hostname>-ip` (when asked), the NIC `<hostname>-nic`, and the VM `<hostname>` with
  its OS disk. The disk, the NIC and the address carry `deleteOption: Delete`, so one forced
  delete of the VM removes all of it. The run deletes only what it made, when it fails or is
  stopped, except a control plane that already answered: a VM create that gets no clear answer is
  looked up by its name and kept only when its `ichor-run` tag is the run's; a NIC or address made
  before a VM that never came is deleted by name. Azure cannot limit a delete to tagged resources,
  so this is enforced in the app, not by the role. The one case that needs the Azure portal is a
  delete that fails: the message names the resource (its id without the subscription) and the
  resource group, and the VM stays billed until it is deleted there.
- **Making a Talos image at Azure (from the Go core and the probe for now):** billable, so the
  estimate gives the helper's hourly price and a note on the image's storage first, and the
  probe's `azure image` runs only with `-yes`. Nothing of the cluster is touched: the schematic
  comes from the template's machine config, and a template whose config cannot be read is refused.
  Before any write the run checks the VM size's architecture, the subnet and the security group
  against the location, that the resource group holds no image of that name (nor, without a
  name, one the app made for this Talos version and schematic), then reads the principal's
  permissions on the resource group, as for a VM. It creates one throwaway Ubuntu VM
  `ichor-image-<run>` with its OS disk, a NIC and a public IP (Standard SKU, to reach the Image
  Factory), and an empty 8 GiB data disk, all tagged `ichor-run`; the VM, through Run Command (no
  SSH, no storage account), downloads the Image Factory's image itself (the phone moves no image
  bytes) and writes it to the data disk. The run makes no image without the script's success
  marker. It then deallocates the VM and makes a managed image from the data disk, tagged
  `os=talos`, `ichor-talos-version`, `ichor-arch` and `ichor-schematic`. On every path it
  force-deletes the VM (its OS disk, NIC and address carry `deleteOption: Delete`), then deletes
  the data disk (`Detach`, so that the image can be made from it), and on a failure or Stop an
  unfinished image. A finished image stays, billed as a managed disk of its size per month until
  you delete it. The one case that needs the Azure portal is a delete that fails: the message
  names each resource left (its id without the subscription) and the resource group.
- **The guided network at Azure (Make a network for the node):** not billable, but it changes the
  subscription, so it runs only when tapped, and the probe's `azure network` only with `-yes`. In
  the picked resource group it makes one network security group and one virtual network, both
  `ichor-nodes` and tagged `ichor-managed`, or reuses them. The group keeps Azure's default rules
  (the network reaches itself, everything else inbound is refused) and adds one rule on plain
  Talos only: TCP 50000 from the phone's current public IPv4 as a /32 (`talos-api-from-phone`),
  refreshed to today's address on each run. It never opens anything to `*` or `Internet`, never
  opens 6443 or 50001, and never deletes the group or the network: nodes share them, so delete
  them in the Azure portal when no node uses them. A group or network named `ichor-nodes` that the
  app did not tag is used as it is and never changed. The run checks the role's network writes
  first and names a missing one. Recorded in the action log (`azure-network-prepare`, with the
  resource group and location).
- **Creating a VM at Google Cloud (Add a node… → A server at a cloud provider → Google Cloud):**
  billable, so the hourly price shows first when the Billing Catalog answers, the host name is
  typed to confirm, and Stop asks once more (the VM and its disk are billed until deleted); the
  probe's `gcp create` runs only with `-yes`. The service account key stays sealed on the device
  and out of backups, and the sheet shows only its email and project; it does not expire and can
  do what its roles allow, so give it only the role the app prints. The token
  it mints (the cloud-platform scope) is kept for the run only, and the key, the token, the key's
  email and id and the project number never appear in a message or the activity log. A key that
  signs in to GKE may still be refused by Compute Engine, so before any write the run checks the
  image (ready, the machine type's architecture, `UEFI_COMPATIBLE` for Arm), the subnet against
  the zone's region, that no instance of the host's name is in the zone, the user data against
  Compute Engine's 256 KiB, then asks `testIamPermissions` on the project: Google has no dry
  run, so a key that lacks a permission the run uses (or whose check cannot be made) is refused
  there, naming the permissions and the role to grant. The run creates one instance named after
  the host name (Talos takes its host name from it), labelled `ichor-run` (the run's id) and
  `ichor-cluster` (a hash of the context name, not the name), with its boot disk set to
  `autoDelete`. It sends one insert with a `requestId`, so a retry after a lost answer is a no-op;
  an insert that gets no clear answer is looked up by name and kept only when its `ichor-run`
  label is the run's. The run deletes only that instance when it fails or is stopped, except a
  control plane that already answered; GCP IAM cannot limit a delete to labelled instances, so
  this is enforced in the app. A delete Google accepted but had not finished within five minutes
  is reported as still billed, never as done. The one case that needs the Google Cloud console is
  a delete that fails: the message names the instance and the zone, and it stays billed until it
  is deleted there.
- **The guided network at Google Cloud (Use a network Ichor sets up):** not billable, but it
  changes the project, so it runs only when tapped, and the probe's `gcp network` only with
  `-yes` (scratch projects only, never in CI). It uses the project's `default` network and makes
  no network or subnet; it adds two VPC firewall rules once, both aimed at the `ichor-node`
  network tag only, so they touch no other VM: `ichor-nodes-internal` (all traffic from tagged
  nodes to tagged nodes) and, on plain Talos, `ichor-phone-50000` (TCP 50000 from the phone's
  current public IPv4 as a /32, described `Ichor phone <date>`). It never opens anything to
  `0.0.0.0/0` or `::/0`, never opens 6443, and never deletes a rule: they are kept, not a run's
  resources, and carry no `ichor-run` label. The phone's address changes (mobile networks, NAT):
  each run adds today's /32 to the rule and keeps the old ones, and the date in its description
  is the last one added, so remove the stale addresses at Google Cloud (VPC network → Firewall)
  when they change. A node that runs but cannot be reached from a new address needs the guided
  network again, never a wider rule. The run asks `testIamPermissions` for the firewall
  permissions before any write and names a missing one. Recorded in the action log
  (`gcp-network-prepare`, with the project, the zone and the rules made).
- **Making a Talos image at Google Cloud (from the Go core and the probe for now):** billable, so
  the estimate gives the helper's hourly price and the disk's and image's storage prices first,
  and the probe's `gcp image` runs only with `-yes`. Nothing of the cluster is touched: the
  schematic comes from the template's machine config, and a template whose config cannot be read
  is refused. Before any write the run checks the machine type (in the zone, of the image's
  architecture), the subnet against the zone's region and its route out (a public address, or
  Cloud NAT: the helper must reach the Image Factory), that the project holds no image of that
  name (without a name: none for this Talos version and schematic), then asks
  `testIamPermissions` on the project, as for a VM. It creates one throwaway Debian VM
  `ichor-image-<run>` with a 10 GB boot disk (`autoDelete`) and an empty 12 GB data disk
  `ichor-image-<run>-data` (kept when the VM goes, so that the image can be made from it), both
  labelled `ichor-run`. Its startup script downloads the Image Factory's image itself (the phone
  moves no image bytes), writes it to the data disk and powers the VM off; no bucket and no
  Storage Transfer are used. The run makes no image without the script's success marker on the
  serial console. It then makes the image from the data disk, labelled `os=talos`,
  `ichor-talos-version`, `ichor-arch` and `ichor-schematic`. On every path it deletes the VM (its
  boot disk goes with it), then the data disk, and on a failure or Stop an unfinished image of
  the run. A finished image stays, billed per GB-month until you delete it. A delete Google has not
  finished within five minutes is reported as still billed, never as done. The one case that
  needs the Google Cloud console is a delete that fails: the message names each resource left
  and the zone.
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

## Reaching the cluster: VPN, firewall and ACLs

Ichor talks straight to the Talos API and the Kubernetes API; it has no relay. How those APIs
are exposed is up to you and outside the app, but a phone moves between networks, so we
recommend never putting them on the open internet:

- **A VPN:** reach the cluster over a private network only.
  - [WireGuard](https://www.wireguard.com/) or [OpenVPN](https://openvpn.net/): a classic
    VPN server in front of the cluster network.
  - [Tailscale](https://tailscale.com/), or a self-hosted alternative:
    [Headscale](https://github.com/juanfont/headscale) (a Tailscale control server you run,
    with the Tailscale apps), [NetBird](https://netbird.io/) or
    [ZeroTier](https://www.zerotier.com/). A mesh VPN needs no open port on the cluster side.
  - **An access proxy instead:** [Teleport](https://goteleport.com/) in front of the
    Kubernetes API (SSO, short-lived certificates, audit), or an SSH bastion giving a SOCKS5
    proxy. Kubeconfig examples for both, and for Tailscale's API proxy:
    [Example kubeconfigs](clusters.md#example-kubeconfigs-proxy-bastion-teleport-tailscale).
  - **In the app:** set the cluster to **VPN only** (cluster switcher → the cluster's menu).
    Without a VPN connected, the app does not try the cluster, and background checks wait.
- **A firewall:** let only the VPN (or your admin networks) reach the API ports:
  - `50000/tcp`: the Talos API (apid), what Ichor uses for Talos clusters;
  - `50001/tcp`: trustd, needed between nodes only, never from a phone;
  - `6443/tcp`: the Kubernetes API.

  On Talos, the [ingress firewall](https://www.talos.dev/latest/talos-guides/network/ingress-firewall/)
  (`NetworkDefaultActionConfig` with `block`, plus `NetworkRuleConfig` rules) does this on the
  nodes themselves; at a cloud provider, use its security groups or firewall too. Keep the
  ports the cluster itself needs open between nodes (etcd, kubelet, the CNI, KubeSpan).
- **ACLs:** in the VPN, allow only the people and devices that need it, and only to those
  ports: Tailscale and Headscale ACLs or grants, NetBird access policies, ZeroTier flow rules,
  or firewall rules per VPN client.
- **Least privilege:** give the phone a talosconfig with the smallest role that does the job
  (`os:reader` to watch, see [Roles](roles.md)), and a Kubernetes user bound by RBAC to what it
  needs. A lost phone then costs less, and its certificate can be left to expire.
