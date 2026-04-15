# Tootles EC2 Network Metadata — Design

Date: 2026-04-15
Status: Design approved, implementation pending

## Goal

Add per-interface network metadata to the tootles EC2 IMDS frontend, modelled on the AWS EC2 metadata `network` key (available since API version `2011-01-01`), so cloud-init's EC2 datasource can configure NICs from IMDS.

## Scope

- **In scope:** EC2 IMDS frontend only (`/2009-04-04/meta-data/network/...`).
- **Out of scope:** HackInstance (rootio) frontend. No mirror of this data to `/metadata`.
- **Out of scope:** Deriving the tree from existing `Hardware.Spec.Interfaces[].DHCP`. Operators populate a new explicit field.

## Consumer

cloud-init's EC2 datasource (`DataSourceEc2.py::convert_ec2_metadata_network_config`). The field list below is bounded by what cloud-init actually reads; AWS-only fields are excluded.

## Schema

### Hardware CR — `api/v1alpha1/tinkerbell/hardware.go`

New field on the existing `MetadataInstance` struct:

```go
type MetadataInstance struct {
    // ... existing fields
    Network *MetadataInstanceNetwork `json:"network,omitempty"`
}

type MetadataInstanceNetwork struct {
    Interfaces *MetadataInstanceNetworkInterfaces `json:"interfaces,omitempty"`
}

type MetadataInstanceNetworkInterfaces struct {
    // Key: lowercase MAC, colon-separated (e.g. "02:aa:bb:cc:dd:ee").
    Macs map[string]*MetadataInstanceNetworkInterface `json:"macs,omitempty"`
}

type MetadataInstanceNetworkInterface struct {
    DeviceNumber         *int64   `json:"device-number,omitempty"`
    InterfaceID          *string  `json:"interface-id,omitempty"`
    LocalHostname        *string  `json:"local-hostname,omitempty"`
    LocalIPv4s           []string `json:"local-ipv4s,omitempty"`
    Mac                  *string  `json:"mac,omitempty"`
    PublicHostname       *string  `json:"public-hostname,omitempty"`
    PublicIPv4s          []string `json:"public-ipv4s,omitempty"`
    SubnetIPv4CidrBlock  *string  `json:"subnet-ipv4-cidr-block,omitempty"`
    VpcIPv4CidrBlocks    []string `json:"vpc-ipv4-cidr-blocks,omitempty"`
    IPv6s                []string `json:"ipv6s,omitempty"`
    SubnetIPv6CidrBlocks []string `json:"subnet-ipv6-cidr-blocks,omitempty"`
    VpcIPv6CidrBlocks    []string `json:"vpc-ipv6-cidr-blocks,omitempty"`
}
```

### Tootles data struct — `pkg/data/instance.go`

Mirror the same shape on `Ec2Instance.Metadata.Network`. The typed JSON round-trip already used in the backend for `storage.raid` (commit `82e7fcab`) picks this up automatically — no converter code to write.

### Fields deliberately excluded

Unused by cloud-init, AWS-specific: `vpc-id`, `subnet-id`, `owner-id`, `security-groups`, `security-group-ids`, `subnet-ipv4-cidr-blocks` (plural sibling of the singular).

## Routing

### Response format

Matches AWS IMDS conventions so cloud-init's `MetadataMaterializer` recurses correctly:

| Path | Response |
|---|---|
| `/meta-data/network/` | `interfaces/\n` |
| `/meta-data/network/interfaces/` | `macs/\n` |
| `/meta-data/network/interfaces/macs/` | one `{mac}/` line per NIC |
| `/meta-data/network/interfaces/macs/{mac}/` | one line per set leaf field |
| `/meta-data/network/interfaces/macs/{mac}/{field}` | scalar value or `\n`-separated list |

Directory entries end with `/`; leaves do not. List leaves (e.g. `local-ipv4s`) are served as newline-separated plain text.

### Implementation

The existing static `dataRoutes` array in `tootles/internal/frontend/ec2/routes.go` cannot express per-MAC dynamic paths. Instead, register a **single subtree handler** at `/meta-data/network/*` that parses the tail path, navigates `Ec2Instance.Metadata.Network`, and returns the appropriate directory listing or leaf value.

New file: `tootles/internal/frontend/ec2/network.go` with its own unit tests. Register the handler for both path prefixes the frontend already serves (`/2009-04-04/...` and `/tootles/instanceID/:instanceID/...`).

### Edge cases

- Unknown MAC → 404
- Known MAC, unset leaf field → 404 (never 200 with empty body — cloud-init would parse empty as valid and produce garbage config)
- Uppercase MAC input normalised to lowercase for lookup; listings always serve lowercase
- No API version gating — this serves under the existing `/2009-04-04/` prefix alongside all other EC2 data, consistent with current behaviour

## Testing

1. **Backend round-trip test** (`pkg/backend/kube/tootles_test.go`) — construct `Hardware` with `Metadata.Instance.Network` for two MACs, run through the backend, assert `Ec2Instance.Metadata.Network` contains both. Same style as the existing raid test.
2. **Subtree handler unit tests** (`tootles/internal/frontend/ec2/network_test.go`) — table-driven: directory listings at each level, scalar leaves, list leaves, unknown MAC, unset field, uppercase MAC normalisation.
3. **End-to-end HTTP test** in the existing frontend test suite — full cloud-init-style recursive walk.
4. **CRD schema** — regenerate with `controller-gen`, commit the updated YAML.

## Commit sequence

Each step independently reviewable:

1. Add `MetadataInstanceNetwork*` types to `api/v1alpha1/tinkerbell/hardware.go` + regenerated CRD YAML.
2. Mirror on `Ec2Instance.Metadata.Network` in `pkg/data/instance.go` + backend round-trip test.
3. Add subtree handler `tootles/internal/frontend/ec2/network.go` + unit tests.
4. Wire the handler into the frontend router for both path prefixes + HTTP-level test.

## YAGNI exclusions

- No MAC-format validation in the CRD (404 handles bad input).
- No CIDR-format validation on `subnet-ipv4-cidr-block` (passed through as written).
- No auto-derivation of the `mac` leaf from the map key. If the operator sets the map key but omits the `Mac` field, `.../{mac}/mac` returns 404.
- No HackInstance mirror.
- No separate `/2011-01-01/` version prefix.

## Caveat: cloud-init behaviour

cloud-init's EC2 datasource does not unconditionally convert IMDS → static network config. It depends on version and `policy` setting. Recent cloud-init (≥ 22.x) converts automatically when DHCP isn't in use. Document this in user-facing docs when the feature ships; not a blocker for the implementation.

## Upstream

This is the canonical `tinkerbell/tinkerbell` repo. Network metadata does not exist upstream today, so this is genuinely new functionality and suitable for upstream contribution.
