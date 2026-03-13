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

Both `--enable-service` and `--auto-service` activate the Service controller. The difference is the default behaviour: in annotation-mode a Service must explicitly opt in; in auto-mode every Service is processed unless explicitly excluded.

## HTTPRoute annotations

Add these to an HTTPRoute to override per-resource behaviour:

| Annotation | Description |
|------------|-------------|
| `newt-sidecar/enabled: "false"` | Skip this HTTPRoute |
| `newt-sidecar/name: "Custom Name"` | Override the resource display name |
| `newt-sidecar/ssl: "false"` | Disable SSL for this resource |

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

### Port selection logic

**Single-port mode** (default, or `newt-sidecar/all-ports: "false"`):

When `newt-sidecar/port` is not set the sidecar selects a port automatically:

1. Service has exactly one port → use it
2. Service has a port named `http` → use it
3. Otherwise the Service is skipped with a warning

**All-ports mode** (`--all-ports` flag or `newt-sidecar/all-ports: "true"`):

Every port defined in the Service spec is exposed as a separate blueprint entry. The protocol is read from the ServicePort spec (`TCP` → `tcp`, `UDP` → `udp`). The `newt-sidecar/port`, `newt-sidecar/protocol`, and `newt-sidecar/name` annotations are ignored in this mode. HTTP mode (`newt-sidecar/full-domain`) is not supported in all-ports mode.

The per-Service annotation always takes precedence over the global flag, so you can opt individual Services in or out regardless of the global default.

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
