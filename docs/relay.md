# Push without polling (ichor-relay)

Ichor checks a cluster's alerts in the background on a schedule the phone decides: Android runs
the checks every 15 minutes at best, and iOS runs them when it chooses. **ichor-relay** is an
optional component you run in the cluster. Alertmanager posts its webhook to it, and it pushes
the alerts to the phones you paired, within seconds:

- **Android:** through a UnifiedPush distributor such as ntfy, encrypted for the phone
  (RFC 8291), so the push server sees nothing readable.
- **iOS:** through the ntfy app, on a random topic Ichor generates.

The background checks keep running as the fallback, and an alert that arrives both ways is
shown once. The relay is one Deployment in its own namespace, with no volume, no Ingress and no
port open outside the cluster.

## Install

Apply the manifests of a release (`vX.Y.Z`: a release tag; every release since the one that
added `deploy/relay` publishes the image):

```bash
kubectl apply -k "https://github.com/cyrinux/ichor/deploy/relay?ref=vX.Y.Z"
kubectl -n ichor-relay rollout status deploy/ichor-relay
```

This creates the namespace `ichor-relay` and, in it, a ServiceAccount, a Role, a Deployment
running `ghcr.io/cyrinux/ichor-relay:vX.Y.Z` (amd64 and arm64), a Service and a NetworkPolicy.
The namespace enforces the `restricted` Pod Security profile; the pod runs as nonroot with a
read-only root filesystem. The manifests are in [deploy/relay](../deploy/relay): copy them into
your GitOps repository if you prefer, and keep the Service's name, namespace and port (8080,
named `http`): the app finds the relay by them.

On its first start the relay creates the Secret `ichor-relay` holding its whole state: a VAPID
key pair, the pairing token, the webhook token and the paired devices. Its Role allows `get`
and `update` on that Secret only, and `create` on Secrets in its own namespace (Kubernetes cannot
limit `create` to one name).

## Point Alertmanager at it

Read the webhook token:

```bash
kubectl -n ichor-relay get secret ichor-relay -o jsonpath='{.data.webhookToken}' | base64 -d
```

Alertmanager runs in another namespace and cannot read that Secret: copy the token into a Secret
next to it (here in `monitoring`):

```bash
kubectl -n monitoring create secret generic ichor-relay-webhook \
  --from-literal=token="$(kubectl -n ichor-relay get secret ichor-relay -o jsonpath='{.data.webhookToken}' | base64 -d)"
```

### alertmanager.yml

Mount that Secret into Alertmanager (here at `/etc/alertmanager/secrets/ichor-relay-webhook`),
then add a receiver and a route. `continue: true` keeps your other receivers getting every
alert; `send_resolved: true` lets the phone clear an alert when it resolves.

```yaml
route:
  routes:
    - receiver: ichor-relay
      continue: true
receivers:
  - name: ichor-relay
    webhook_configs:
      - url: http://ichor-relay.ichor-relay.svc:8080/alertmanager
        send_resolved: true
        http_config:
          authorization:
            credentials_file: /etc/alertmanager/secrets/ichor-relay-webhook/token
```

`credentials: <token>` in place of `credentials_file` works too, with the token in the file.

### prometheus-operator (AlertmanagerConfig)

```yaml
apiVersion: monitoring.coreos.com/v1alpha1
kind: AlertmanagerConfig
metadata:
  name: ichor-relay
  namespace: monitoring
spec:
  route:
    receiver: ichor-relay
    continue: true
  receivers:
    - name: ichor-relay
      webhookConfigs:
        - url: http://ichor-relay.ichor-relay.svc:8080/alertmanager
          sendResolved: true
          httpConfig:
            authorization:
              type: Bearer
              credentials:
                name: ichor-relay-webhook
                key: token
```

The operator adds a `namespace="monitoring"` matcher to an AlertmanagerConfig's route by
default, so it only gets the alerts of its own namespace. To send every alert, set
`alertmanagerConfigMatcherStrategy: {type: None}` on the Alertmanager resource, or put the
receiver in the Alertmanager's main configuration instead.

Check it: the relay answers 202 to Alertmanager. `kubectl -n ichor-relay logs deploy/ichor-relay`
lists every push it sends, by device ID and HTTP status (never a token, a push endpoint or a
label).

## Pair a phone

Pairing goes through the Kubernetes API server's service proxy with the phone's own credentials,
the way Ichor reads Alertmanager: whoever may read the Secret `ichor-relay` may pair (see
[Roles](roles.md)). Pair each cluster on each phone; unpaired, the background checks still alert
as before.

**Android.** Install a UnifiedPush distributor: ntfy (from F-Droid or Google Play) or any other
([the list](https://unifiedpush.org/users/distributors/), also behind **Get a distributor** in
the app). Then **Settings → Alerts**: turn on **Alertmanager alerts**, which shows the row
**Push from ichor-relay**, and turn that on. With
several distributors installed, Ichor asks which one to receive through; the row then says
**Paired · via** that distributor. Messages are encrypted for the phone (RFC 8291) before they
leave the relay, so the distributor's push server sees nothing readable, and Ichor shows them
as its own alert notifications, under the alert rules like the ones of the background check: a
push inside the cluster's quiet hours or below its minimum severity is not shown, and the
"Since you last looked" card says why. If the row says to update your distributor, the one
installed cannot decrypt the relay's messages.

**iOS.** UnifiedPush does not exist on iOS, so the relay publishes to a topic on an ntfy server
and the ntfy app shows the notification. Install the ntfy app, then **Settings → Alerts → Push
through ntfy**: keep the random topic Ichor generates (anyone who knows a topic can read it),
**Copy** it, subscribe to it in the ntfy app, then **Pair**. These messages are not end-to-end
encrypted: the ntfy server sees the title (Ichor, the cluster's nickname and the firing and
critical counts) and one line per alert with its severity, name and namespace; with **Include alert
text** it also sees each alert's summary. Prefer your own ntfy server: one you host needs
[`upstream-base-url`](https://docs.ntfy.sh/config/#ios-instant-notifications) set for the iOS
app to deliver instantly. Tapping the notification opens Ichor on the cluster's Alerts screen.

## An ntfy server in the cluster

The relay pushes over https to public addresses only. For an ntfy server inside the cluster, set
the Deployment's `RELAY_ALLOW_PRIVATE` to `"true"` (https to a private address) or
`RELAY_ALLOW_HTTP` to `"true"` (plain http, which also allows private addresses), and add the
server's port to the egress rules of `networkpolicy.yaml` when it is not 443. Link-local
addresses, such as a cloud's metadata service, are refused whatever the setting.

## Update and remove

Apply the manifests of the new release the same way; the Secret, and so the paired phones, stay.
`kubectl delete -k "https://github.com/cyrinux/ichor/deploy/relay?ref=vX.Y.Z"` removes
everything, the Secret included: phones then need pairing again with a new relay.

To build the image from a checkout: `just relay-image` (`localhost/ichor-relay:dev`).

What leaves the cluster, who sees what, and how to rotate each token:
[Trust model of ichor-relay](../SECURITY.md#trust-model-of-ichor-relay).
