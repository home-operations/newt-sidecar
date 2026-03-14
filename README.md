# newt-sidecar

A Kubernetes sidecar that watches HTTPRoute and/or Service resources and dynamically generates a [Pangolin](https://github.com/fosrl/pangolin) blueprint YAML for the [newt](https://github.com/fosrl/newt) tunnel daemon.

## How it works

The sidecar runs alongside newt in the same pod, sharing a volume. It watches HTTPRoutes referencing a configured gateway and/or Services with the appropriate annotations, and writes `/etc/newt/blueprint.yaml` whenever resources change. newt detects the file change and updates the tunnel accordingly.

```
┌─────────────────────────────────────────┐
│  Newt Pod                               │
│  ┌────────────────┐  ┌───────────────┐  │
│  │  newt-sidecar  │  │     newt      │  │
│  │  (watches      │  │  (reads       │  │
│  │  HTTPRoutes +  │  │  blueprint)   │  │
│  │  Services)     │  │               │  │
│  └───────┬────────┘  └──────┬────────┘  │
│          │   emptyDir vol   │           │
│          └──► blueprint ◄───┘           │
│                .yaml                    │
└─────────────────────────────────────────┘
```

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--gateway-name` | `""` | Gateway name to filter HTTPRoutes. When omitted the HTTPRoute controller is disabled |
| `--gateway-namespace` | `""` | Gateway namespace (empty = any) |
| `--namespace` | `""` | Watch namespace (empty = all) |
| `--output` | `/etc/newt/blueprint.yaml` | Output blueprint file path |
| `--site-id` | `""` | Pangolin site nice ID (required) |
| `--target-hostname` | `""` | Backend gateway hostname (required for HTTPRoute mode) |
| `--target-port` | `443` | Backend gateway port |
| `--target-method` | `https` | Backend method (`http`/`https`/`h2c`) |
| `--deny-countries` | `""` | Comma-separated country codes to deny |
| `--ssl` | `true` | Default SSL setting for http/https resources |
| `--annotation-prefix` | `newt-sidecar` | Annotation prefix for per-resource overrides |
| `--enable-service` | `false` | Enable Service discovery (annotation-mode: opt-in via `newt-sidecar/enabled: "true"`) |
| `--auto-service` | `false` | Enable Service discovery (auto-mode: opt-out via `newt-sidecar/enabled: "false"`) |
| `--all-ports` | `false` | Expose all TCP/UDP ports of a Service as individual blueprint entries (global default, overridable per Service via `newt-sidecar/all-ports` annotation) |
| `--auth-sso-roles` | `""` | Default comma-separated Pangolin roles for SSO-enabled resources (empty = none) |
| `--auth-sso-users` | `""` | Default comma-separated user e-mails for SSO-enabled resources (empty = none) |
| `--auth-sso-idp` | `0` | Default Pangolin IdP ID for `auto-login-idp` (`0` = not set) |
| `--auth-whitelist-users` | `""` | Default comma-separated user e-mails for `whitelist-users` (empty = none) |

Both `--enable-service` and `--auto-service` activate the Service controller. The difference is the default behaviour: in annotation-mode a Service must explicitly opt in; in auto-mode every Service is processed unless explicitly excluded.

There is deliberately no `--auth-sso` global flag. SSO must be enabled explicitly per resource via the `newt-sidecar/auth-sso` annotation so that resources remain public unless opted in.

There are deliberately no global flags for `--auth-pincode`, `--auth-password`, or `--auth-basic-auth-*`. Sensitive auth values must be stored in a Kubernetes Secret and referenced via the `newt-sidecar/auth-secret` annotation (see [Auth via Kubernetes Secret](#auth-via-kubernetes-secret)).

## HTTPRoute annotations

Add these to an HTTPRoute to override per-resource behaviour:

| Annotation | Description |
|------------|-------------|
| `newt-sidecar/enabled: "false"` | Skip this HTTPRoute entirely |
| `newt-sidecar/name: "Custom Name"` | Override the resource display name |
| `newt-sidecar/ssl: "false"` | Disable SSL for this resource |
| `newt-sidecar/host-header: "custom.internal"` | Set the `host-header` field on the Pangolin resource |
| `newt-sidecar/headers: '[{"name":"X-Foo","value":"bar"}]'` | JSON array of extra headers to pass to Pangolin |
| `newt-sidecar/auth-sso: "true"` | Enable SSO authentication |
| `newt-sidecar/auth-sso-roles: "Member,Developer"` | Comma-separated Pangolin roles allowed (overrides `--auth-sso-roles`) |
| `newt-sidecar/auth-sso-users: "user@example.com"` | Comma-separated user e-mails allowed (overrides `--auth-sso-users`) |
| `newt-sidecar/auth-sso-idp: "1"` | Pangolin IdP ID for `auto-login-idp` — skips the Pangolin login page and redirects directly to the IdP (overrides `--auth-sso-idp`) |
| `newt-sidecar/auth-whitelist-users: "user@example.com"` | Comma-separated user e-mails for `whitelist-users` (overrides `--auth-whitelist-users`) |
| `newt-sidecar/auth-secret: "my-secret"` | Name of a Kubernetes Secret in the same namespace containing sensitive auth values (see [Auth via Kubernetes Secret](#auth-via-kubernetes-secret)) |
| `newt-sidecar/tls-server-name: "backend.internal"` | Override the SNI name for the backend TLS connection (defaults to the HTTPRoute hostname) |
| `newt-sidecar/maintenance-enabled: "true"` | Enable the Pangolin maintenance block |
| `newt-sidecar/maintenance-type: "forced"` | Maintenance type: `forced` or `automatic` |
| `newt-sidecar/maintenance-title: "Down for maintenance"` | Maintenance page title |
| `newt-sidecar/maintenance-message: "Back soon"` | Maintenance page message |
| `newt-sidecar/maintenance-estimated-time: "2h"` | Estimated maintenance duration |
| `newt-sidecar/target-path: "/api"` | Path prefix, exact path, or regex pattern for the target |
| `newt-sidecar/target-path-match: "prefix"` | Path matching type: `prefix`, `exact`, or `regex` |
| `newt-sidecar/target-rewrite-path: "/"` | Path to rewrite the request to |
| `newt-sidecar/target-rewrite-match: "stripPrefix"` | Rewrite matching type: `exact`, `prefix`, `regex`, or `stripPrefix` |
| `newt-sidecar/target-priority: "200"` | Target priority for load balancing (1–1000, default 100) |
| `newt-sidecar/target-internal-port: "8080"` | Internal port mapping on the target (1–65535) |
| `newt-sidecar/target-healthcheck: '{"hostname":"...","port":8080}'` | JSON health check configuration for the target (see [Health check annotation](#health-check-annotation)) |
| `newt-sidecar/rules: '[{"action":"deny","match":"ip","value":"10.0.0.0/8"}]'` | JSON array of custom access control rules (see [Custom rules](#custom-rules)) |
| `newt-sidecar/target-enabled: "true"` | Enable or disable the target: `"true"`/`"1"` or `"false"`/`"0"` |

### Finding the IdP ID

The `auto-login-idp` value is the internal numeric ID Pangolin assigns to each configured Identity Provider. You can find it in two ways:

1. **Pangolin UI** — navigate to *Server Admin → Identity Providers*, click an IdP to edit it, and read the number from the URL: `.../admin/idp/**1**/general`
2. **Pangolin API** — `GET /api/v1/idp` returns `idpId`, `name`, and `type` for every configured IdP

## Service annotations

Services can be exposed in two modes depending on whether `newt-sidecar/full-domain` is set.

### TCP/UDP mode (default)

Pangolin opens a raw TCP or UDP port and tunnels directly to the cluster-internal Service DNS — no Envoy Gateway hop.

| Annotation | Default | Description |
|------------|---------|-------------|
| `newt-sidecar/enabled` | — | `"true"` to opt in (annotation-mode); `"false"` to opt out (auto-mode) |
| `newt-sidecar/all-ports` | `--all-ports` flag | `"true"` to expose all ports as individual entries; `"false"` to force single-port mode. Overrides the global `--all-ports` flag |
| `newt-sidecar/port` | auto | Port number or name to expose (single-port mode only). Required when the Service has more than one port and none is named `http` |
| `newt-sidecar/protocol` | from spec | Tunnel protocol override: `tcp` or `udp` (single-port mode only). Defaults to the protocol defined in the ServicePort spec |
| `newt-sidecar/name` | `<svc> <port>` | Override the resource display name (single-port mode only) |

### HTTP mode

Set `newt-sidecar/full-domain` to switch to HTTP mode. Pangolin exposes the Service at the given public domain over HTTPS. The internal target is the cluster-internal Service DNS name — no Envoy Gateway hop. HTTP mode is not supported in all-ports mode.

| Annotation | Default | Description |
|------------|---------|-------------|
| `newt-sidecar/enabled` | — | `"true"` to opt in (annotation-mode); `"false"` to opt out (auto-mode) |
| `newt-sidecar/full-domain` | — | Public domain to expose (e.g. `app.example.com`). Activates HTTP mode |
| `newt-sidecar/port` | auto | Port number or name to expose |
| `newt-sidecar/method` | `http` | Internal protocol to reach the Service: `http`, `https`, or `h2c` |
| `newt-sidecar/ssl` | `--ssl` flag | Enable SSL on the Pangolin resource |
| `newt-sidecar/name` | `<svc> <port>` | Override the resource display name |
| `newt-sidecar/host-header` | — | Set the `host-header` field on the Pangolin resource |
| `newt-sidecar/headers` | — | JSON array of extra headers: `[{"name":"X-Foo","value":"bar"}]` |
| `newt-sidecar/auth-sso` | — | `"true"` to enable SSO authentication |
| `newt-sidecar/auth-sso-roles` | `--auth-sso-roles` | Comma-separated Pangolin roles (overrides global default) |
| `newt-sidecar/auth-sso-users` | `--auth-sso-users` | Comma-separated user e-mails (overrides global default) |
| `newt-sidecar/auth-sso-idp` | `--auth-sso-idp` | Pangolin IdP ID for `auto-login-idp` (overrides global default) |
| `newt-sidecar/auth-whitelist-users` | `--auth-whitelist-users` | Comma-separated user e-mails for `whitelist-users` (overrides global default) |
| `newt-sidecar/auth-secret` | — | Name of a Kubernetes Secret containing sensitive auth values (see below) |
| `newt-sidecar/tls-server-name` | FullDomain | Override the SNI name for the backend TLS connection |
| `newt-sidecar/maintenance-enabled` | — | `"true"` to enable the Pangolin maintenance block |
| `newt-sidecar/maintenance-type` | — | `forced` or `automatic` |
| `newt-sidecar/maintenance-title` | — | Maintenance page title |
| `newt-sidecar/maintenance-message` | — | Maintenance page message |
| `newt-sidecar/maintenance-estimated-time` | — | Estimated maintenance duration |
| `newt-sidecar/target-path` | — | Path prefix, exact path, or regex pattern for the target |
| `newt-sidecar/target-path-match` | — | Path matching type: `prefix`, `exact`, or `regex` |
| `newt-sidecar/target-rewrite-path` | — | Path to rewrite the request to |
| `newt-sidecar/target-rewrite-match` | — | Rewrite matching type: `exact`, `prefix`, `regex`, or `stripPrefix` |
| `newt-sidecar/target-priority` | `100` | Target priority for load balancing (1–1000) |
| `newt-sidecar/target-internal-port` | — | Internal port mapping on the target (1–65535) |
| `newt-sidecar/target-healthcheck` | — | JSON health check config for the target (see [Health check annotation](#health-check-annotation)) |
| `newt-sidecar/rules` | — | JSON array of custom access control rules (see [Custom rules](#custom-rules)) |
| `newt-sidecar/target-enabled` | — | Enable or disable the target: `"true"`/`"1"` or `"false"`/`"0"` |

### Port selection logic

**Single-port mode** (default, or `newt-sidecar/all-ports: "false"`):

When `newt-sidecar/port` is not set the sidecar selects a port automatically:

1. Service has exactly one port → use it
2. Service has a port named `http` → use it
3. Otherwise the Service is skipped with a warning

**All-ports mode** (`--all-ports` flag or `newt-sidecar/all-ports: "true"`):

Every port defined in the Service spec is exposed as a separate blueprint entry. The protocol is read from the ServicePort spec (`TCP` → `tcp`, `UDP` → `udp`). The `newt-sidecar/port`, `newt-sidecar/protocol`, and `newt-sidecar/name` annotations are ignored in this mode. HTTP mode (`newt-sidecar/full-domain`) is not supported in all-ports mode.

The per-Service annotation always takes precedence over the global flag, so you can opt individual Services in or out regardless of the global default.

## Health check annotation

The `newt-sidecar/target-healthcheck` annotation accepts a JSON object matching the Pangolin `healthcheck` spec:

```yaml
annotations:
  newt-sidecar/full-domain: "app.example.com"
  newt-sidecar/target-healthcheck: |
    {
      "hostname": "app.default.svc.cluster.local",
      "port": 8080,
      "enabled": true,
      "path": "/health",
      "interval": 30,
      "timeout": 5,
      "method": "GET",
      "status": 200
    }
```

All fields are optional except `hostname` and `port`. The full schema:

| Field | Type | Description |
|-------|------|-------------|
| `hostname` | string | Hostname to health-check |
| `port` | number | Port to health-check |
| `enabled` | boolean | Whether health checking is active (default `true`) |
| `path` | string | HTTP path to request |
| `scheme` | string | Protocol scheme |
| `mode` | string | Health check mode (default `http`) |
| `interval` | number | Seconds between checks (default 30) |
| `unhealthy-interval` | number | Seconds between checks when unhealthy (default 30) |
| `timeout` | number | Timeout in seconds (default 5) |
| `headers` | array | Extra headers: `[{"name":"…","value":"…"}]` |
| `follow-redirects` | boolean | Whether to follow redirects (default `true`) |
| `method` | string | HTTP method (default `GET`) |
| `status` | number | Expected HTTP status code |

## Custom rules

The `{prefix}/rules` annotation accepts a JSON array of custom access control rules. Rules are evaluated in priority order (lower number = higher priority). Each rule has:

- `action`: `allow`, `deny`, or `pass`
- `match`: `cidr`, `ip`, `path`, or `country`
- `value`: the match value (CIDR, IP, path pattern, or country code)
- `priority` (optional): defaults to 100 if not specified

**Example:**

```yaml
annotations:
  newt-sidecar/rules: '[{"action":"deny","match":"ip","value":"10.0.0.0/8"},{"action":"allow","match":"path","value":"/admin","priority":10}]'
```

**Valid combinations:**

| `match` | `value` example | Description |
|---|---|---|
| `cidr` | `10.0.0.0/8` | CIDR block |
| `ip` | `192.168.1.1` | Single IP address |
| `path` | `/admin` | Path pattern |
| `country` | `RU` | Country code (2 letters) |

The `{prefix}/rules` annotation is merged with the `--deny-countries` flag rules: annotation rules come first, then country-deny rules are appended.

## Private resources

The Pangolin blueprint supports a `private-resources` block for Pangolin client access (SSH, RDP, CIDR tunnels). The data types are fully defined in the sidecar's blueprint package and will be serialised correctly if populated, but **private resources are not auto-generated from Kubernetes resource annotations**. There is no standard Kubernetes resource type that maps cleanly to a Pangolin private resource.

To include private resources in the generated blueprint you would need a separate input mechanism (e.g. a ConfigMap or custom controller) that is out of scope for the current annotation-driven approach.

## Auth via Kubernetes Secret

Sensitive auth values — pincode, password, and basic-auth credentials — are never read from annotations. Instead, create a Kubernetes Secret in the same namespace as the resource and reference it with the `newt-sidecar/auth-secret` annotation.

**Well-known Secret keys:**

| Key | Auth field |
|-----|------------|
| `pincode` | `auth.pincode` (parsed as integer) |
| `password` | `auth.password` |
| `basic-auth-user` | `auth.basic-auth.user` |
| `basic-auth-password` | `auth.basic-auth.password` |

**Example Secret:**

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: myapp-auth
  namespace: default
stringData:
  password: "s3cr3t"
```

**Reference it from an HTTPRoute or Service:**

```yaml
annotations:
  newt-sidecar/auth-secret: "myapp-auth"
```

The Secret may contain any subset of the well-known keys. Keys that are absent or empty are ignored.

> **RBAC**: the sidecar's ServiceAccount needs `get` on `secrets` in each watched namespace when `auth-secret` is used. See the [RBAC example](#helmrelease-httproute--auth-secret) below.

## Kubernetes deployment

Deploy using the [bjw-s app-template](https://bjw-s-labs.github.io/helm-charts/docs/app-template/) chart. The sidecar runs as a native Kubernetes sidecar (`initContainer` with `restartPolicy: Always`, requires K8s 1.29+). An `emptyDir` volume is shared between the sidecar and newt at `/etc/newt`.

### OCIRepository

```yaml
apiVersion: source.toolkit.fluxcd.io/v1beta2
kind: OCIRepository
metadata:
  name: newt
spec:
  interval: 1h
  url: oci://ghcr.io/bjw-s-labs/helm/app-template
  ref:
    tag: 4.6.2
```

### HelmRelease (HTTPRoute only)

```yaml
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: newt
spec:
  chartRef:
    kind: OCIRepository
    name: newt
  interval: 1h
  values:
    defaultPodOptions:
      securityContext:
        runAsNonRoot: true
        runAsUser: 1000
        runAsGroup: 1000
    controllers:
      newt:
        replicas: 2
        initContainers:
          newt-sidecar:
            image:
              repository: ghcr.io/home-operations/newt-sidecar
              tag: latest
            args:
              - --gateway-name=kgateway-external
              - --site-id=<pangolin-site-id>
              - --target-hostname=kgateway-external.network.svc.cluster.local
              - --deny-countries=RU,CN,KP,IR,BY,IL
            restartPolicy: Always
            resources:
              limits:
                memory: 64Mi
        containers:
          app:
            image:
              repository: fosrl/newt
              tag: 1.10.1
            env:
              PANGOLIN_ENDPOINT:
                valueFrom:
                  secretKeyRef:
                    name: newt-secret
                    key: NEWT_SERVER_URL
              NEWT_ID:
                valueFrom:
                  secretKeyRef:
                    name: newt-secret
                    key: NEWT_ID
              NEWT_SECRET:
                valueFrom:
                  secretKeyRef:
                    name: newt-secret
                    key: NEWT_SECRET
              BLUEPRINT_FILE: /etc/newt/blueprint.yaml
            securityContext:
              allowPrivilegeEscalation: false
              readOnlyRootFilesystem: true
              capabilities: {drop: ["ALL"]}
            resources:
              requests:
                cpu: 10m
              limits:
                memory: 256Mi
    rbac:
      roles:
        newt:
          type: ClusterRole
          rules:
            - apiGroups:
                - gateway.networking.k8s.io
              resources:
                - httproutes
              verbs:
                - get
                - watch
                - list
      bindings:
        newt:
          type: ClusterRoleBinding
          roleRef:
            identifier: newt
          subjects:
            - identifier: newt
    serviceAccount:
      newt: {}
    persistence:
      blueprint:
        type: emptyDir
        globalMounts:
          - path: /etc/newt
```

### HelmRelease (HTTPRoute + Service discovery)

Add `--enable-service` (or `--auto-service`) to the sidecar args and extend the ClusterRole to include Services:

```yaml
args:
  - --gateway-name=kgateway-external
  - --site-id=<pangolin-site-id>
  - --target-hostname=kgateway-external.network.svc.cluster.local
  - --deny-countries=RU,CN,KP,IR,BY,IL
  - --enable-service
```

```yaml
rbac:
  roles:
    newt:
      type: ClusterRole
      rules:
        - apiGroups:
            - gateway.networking.k8s.io
          resources:
            - httproutes
          verbs:
            - get
            - watch
            - list
        - apiGroups:
            - ""
          resources:
            - services
          verbs:
            - get
            - watch
            - list
```

### HelmRelease (HTTPRoute + auth-secret)

When any resource uses `newt-sidecar/auth-secret`, add a Role (not ClusterRole) in each watched namespace that grants `get` on Secrets, and bind it to the ServiceAccount:

```yaml
rbac:
  roles:
    newt:
      type: ClusterRole
      rules:
        - apiGroups:
            - gateway.networking.k8s.io
          resources:
            - httproutes
          verbs:
            - get
            - watch
            - list
    newt-secrets:
      type: Role
      rules:
        - apiGroups:
            - ""
          resources:
            - secrets
          verbs:
            - get
  bindings:
    newt:
      type: ClusterRoleBinding
      roleRef:
        identifier: newt
      subjects:
        - identifier: newt
    newt-secrets:
      type: RoleBinding
      roleRef:
        identifier: newt-secrets
      subjects:
        - identifier: newt
```

### Service examples

TCP tunnel (e.g. PostgreSQL):

```yaml
apiVersion: v1
kind: Service
metadata:
  name: postgres
  namespace: default
  annotations:
    newt-sidecar/enabled: "true"
spec:
  ports:
    - name: postgres
      port: 5432
```

HTTP tunnel (direct Service, no gateway hop):

```yaml
apiVersion: v1
kind: Service
metadata:
  name: myapp
  namespace: default
  annotations:
    newt-sidecar/enabled: "true"
    newt-sidecar/full-domain: "myapp.example.com"
    newt-sidecar/name: "My App"
spec:
  ports:
    - name: http
      port: 8080
```

HTTP tunnel with SSO (auto-login to IdP 1, role `Member` required):

```yaml
apiVersion: v1
kind: Service
metadata:
  name: myapp
  namespace: default
  annotations:
    newt-sidecar/enabled: "true"
    newt-sidecar/full-domain: "myapp.example.com"
    newt-sidecar/auth-sso: "true"
    newt-sidecar/auth-sso-roles: "Member"
    newt-sidecar/auth-sso-idp: "1"
spec:
  ports:
    - name: http
      port: 8080
```

HTTP tunnel with password auth (from a Secret):

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: myapp-auth
  namespace: default
stringData:
  password: "s3cr3t"
---
apiVersion: v1
kind: Service
metadata:
  name: myapp
  namespace: default
  annotations:
    newt-sidecar/enabled: "true"
    newt-sidecar/full-domain: "myapp.example.com"
    newt-sidecar/auth-secret: "myapp-auth"
spec:
  ports:
    - name: http
      port: 8080
```

All-ports TCP/UDP tunnel (e.g. expose every port of a multi-port Service):

```yaml
apiVersion: v1
kind: Service
metadata:
  name: gameserver
  namespace: default
  annotations:
    newt-sidecar/enabled: "true"
    newt-sidecar/all-ports: "true"
spec:
  ports:
    - name: tcp-game
      port: 7777
      protocol: TCP
    - name: udp-game
      port: 7778
      protocol: UDP
```
