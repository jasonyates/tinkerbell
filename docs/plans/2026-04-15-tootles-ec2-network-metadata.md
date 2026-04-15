# Tootles EC2 Network Metadata Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add per-interface network metadata to the tootles EC2 IMDS frontend, modelled on AWS EC2's `network` key (API version 2011-01-01), so cloud-init's EC2 datasource can configure NICs from IMDS.

**Architecture:** New `Metadata.Instance.Network` field on the `Hardware` CR (mirror of the raid passthrough pattern). Parallel Go struct on `Ec2Instance.Metadata.Network` in `pkg/data`. Hand-written conversion in `tootles/internal/backend.toEC2Instance` (this converter is NOT a JSON round-trip — unlike `toHackInstance`). A new subtree handler in `tootles/internal/frontend/ec2/network.go` serves the dynamic per-MAC routes under `/2009-04-04/meta-data/network/...`.

**Tech Stack:** Go 1.25, Kubernetes CRDs (controller-gen v0.20.0), gin router, gomock for frontend tests.

**Design doc:** `docs/plans/2026-04-15-tootles-ec2-network-metadata-design.md`

**Branch:** `feat/metadata-network` (already created, design doc committed).

---

## Pre-flight

Before starting, confirm the build is green:

```bash
go build ./... 2>&1 | tail -5
```

**Known pre-existing failure:** `pkg/backend/kube/tootles_test.go` is broken (imports non-existent `pkg/api/v1alpha1/tinkerbell`, calls `toHackInstance` which lives in a different package). Task 0 removes it. The useful parts of that test are already covered by `tootles/internal/backend/backend_test.go`.

---

## Task 0: Remove the broken raid test file

**Files:**
- Delete: `pkg/backend/kube/tootles_test.go`

**Step 1: Verify the breakage**

Run: `go build ./pkg/backend/kube/... 2>&1 | tail -3`

Expected: error `no required module provides package github.com/tinkerbell/tinkerbell/pkg/api/v1alpha1/tinkerbell`.

**Step 2: Confirm no other references**

Run: `grep -rn "toHackInstance\|pkg/backend/kube/tootles" --include="*.go" .`

Expected: only references in the test file being deleted and the legitimate one in `tootles/internal/backend/backend.go`.

**Step 3: Delete**

```bash
rm pkg/backend/kube/tootles_test.go
```

**Step 4: Verify build now passes**

Run: `go build ./... 2>&1 | tail -5`

Expected: no errors.

**Step 5: Commit**

```bash
git add -u pkg/backend/kube/tootles_test.go
git commit -m "test: remove broken orphan test in pkg/backend/kube

The file imports a non-existent package and calls toHackInstance, which
lives in tootles/internal/backend. Raid passthrough is already covered
by tootles/internal/backend/backend_test.go."
```

---

## Task 1: Add `MetadataInstanceNetwork` types to the Hardware CR

**Files:**
- Modify: `api/v1alpha1/tinkerbell/hardware.go` (add `Network` field to `MetadataInstance` and add four new types)

**Step 1: Add the field to `MetadataInstance`**

In `MetadataInstance` (currently lines 253–269), add below `NetworkReady`:

```go
Network      *MetadataInstanceNetwork         `json:"network,omitempty"`
```

**Step 2: Add the new types after `MetadataInstanceStorageMountFilesystemOptions` (end of file)**

```go
// MetadataInstanceNetwork exposes per-interface network configuration, modelled on
// AWS EC2 instance metadata's network key (API version 2011-01-01 onward).
// It is consumed by the tootles EC2 IMDS frontend so cloud-init's EC2 datasource
// can configure NICs from IMDS.
type MetadataInstanceNetwork struct {
	Interfaces *MetadataInstanceNetworkInterfaces `json:"interfaces,omitempty"`
}

type MetadataInstanceNetworkInterfaces struct {
	// Macs maps lowercase, colon-separated MAC addresses (e.g. "02:aa:bb:cc:dd:ee")
	// to their per-interface configuration. The key is the URL segment used in the
	// IMDS tree at /meta-data/network/interfaces/macs/{mac}/.
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

Rationale for pointer scalars: distinguishes "not set" (→ 404) from "set to empty/zero" (→ 200 with empty body). Slice fields use `nil` vs non-nil (empty slice still counts as set if the operator explicitly provided it).

**Step 3: Run DeepCopy regen — needed because these are CRD types**

Check if `zz_generated.deepcopy.go` exists alongside `hardware.go`:

```bash
ls api/v1alpha1/tinkerbell/ | grep deepcopy
```

If yes, it will be regenerated by `make manifests` in Task 2 (controller-gen produces both CRDs and deepcopy). If no DeepCopy file, skip.

**Step 4: Run `go build`**

Run: `go build ./api/...`
Expected: no errors.

**Step 5: Commit**

```bash
git add api/v1alpha1/tinkerbell/hardware.go
git commit -m "api: add MetadataInstanceNetwork to Hardware spec

Adds a Network field to MetadataInstance, plus three new types to
represent per-interface network metadata. Field names and JSON tags
match the AWS EC2 instance metadata network/ tree (API 2011-01-01)
that cloud-init's EC2 datasource consumes.

This change is purely additive; the new field is optional and no
existing Hardware resources are affected."
```

---

## Task 2: Regenerate CRDs and deepcopy

**Files:**
- Modify: `helm/tinkerbell/charts/tinkerbell-crds/crds/*.yaml` (auto-generated)
- Modify: `api/v1alpha1/tinkerbell/zz_generated.deepcopy.go` (auto-generated, if present)

**Step 1: Run manifests target**

Run: `make manifests`

Expected: success. Lists regenerated files.

**Step 2: Inspect diff — sanity check**

Run: `git diff --stat`

Expected: modified CRD YAML files under `helm/.../crds/` and possibly `zz_generated.deepcopy.go`. No other changes.

Skim the CRD diff to confirm `network` appears under `metadata.instance` in the hardware CRD:

Run: `git diff helm/tinkerbell/charts/tinkerbell-crds/crds/ | grep -A2 '^\+.*network:'` 

Expected: shows the new `network` schema additions.

**Step 3: Build + vet everything**

Run: `go build ./... && go vet ./...`
Expected: no errors.

**Step 4: Commit**

```bash
git add helm/tinkerbell/charts/tinkerbell-crds/crds/ api/v1alpha1/tinkerbell/zz_generated.deepcopy.go
git commit -m "api: regenerate CRDs and deepcopy for MetadataInstanceNetwork"
```

---

## Task 3: Add parallel Network types to `Ec2Instance`

**Files:**
- Modify: `pkg/data/instance.go`

`pkg/data` is the tootles-internal data model with no JSON tags (the EC2 frontend serves scalar fields, not JSON). We define parallel Go types here so the backend conversion has a target.

**Step 1: Add `Network` field to `Metadata` (currently lines 16–29)**

Below `OperatingSystem OperatingSystem`:

```go
Network         Network
```

**Step 2: Add the new types after `LicenseActivation` (end of the Ec2Instance section, before `HackInstance`)**

```go
// Network is part of Metadata. Mirrors the AWS EC2 metadata network/ tree.
type Network struct {
	// Interfaces is keyed by lowercase, colon-separated MAC address.
	Interfaces map[string]NetworkInterface
}

// NetworkInterface holds the per-NIC fields served under
// /meta-data/network/interfaces/macs/{mac}/. Scalar pointers distinguish
// "not set" (→ 404) from "set to empty string" (→ 200 with empty body).
type NetworkInterface struct {
	DeviceNumber         *int64
	InterfaceID          *string
	LocalHostname        *string
	LocalIPv4s           []string
	Mac                  *string
	PublicHostname       *string
	PublicIPv4s          []string
	SubnetIPv4CidrBlock  *string
	VpcIPv4CidrBlocks    []string
	IPv6s                []string
	SubnetIPv6CidrBlocks []string
	VpcIPv6CidrBlocks    []string
}
```

**Step 3: Build**

Run: `go build ./pkg/data/...`
Expected: no errors.

**Step 4: Commit**

```bash
git add pkg/data/instance.go
git commit -m "data: add Network struct to Ec2Instance.Metadata

Parallel of the Hardware CR's MetadataInstanceNetwork. Consumed by the
EC2 IMDS frontend; populated by the backend converter."
```

---

## Task 4: Extend `toEC2Instance` to copy Network + test

**Files:**
- Modify: `tootles/internal/backend/backend.go` (function `toEC2Instance`, currently lines 103–149)
- Modify: `tootles/internal/backend/backend_test.go`

### Step 1: Write the failing test (TDD)

In `backend_test.go`, add a new test function at the end of the file:

```go
func TestGetEC2Instance_PassesThroughNetwork(t *testing.T) {
	ptr := func(s string) *string { return &s }
	deviceNumber := int64(0)
	hw := &v1alpha1.Hardware{
		Spec: v1alpha1.HardwareSpec{
			Metadata: &v1alpha1.HardwareMetadata{
				Instance: &v1alpha1.MetadataInstance{
					Network: &v1alpha1.MetadataInstanceNetwork{
						Interfaces: &v1alpha1.MetadataInstanceNetworkInterfaces{
							Macs: map[string]*v1alpha1.MetadataInstanceNetworkInterface{
								"02:aa:bb:cc:dd:ee": {
									DeviceNumber:        &deviceNumber,
									InterfaceID:         ptr("eni-abc"),
									Mac:                 ptr("02:aa:bb:cc:dd:ee"),
									LocalIPv4s:          []string{"10.0.0.5"},
									SubnetIPv4CidrBlock: ptr("10.0.0.0/24"),
									VpcIPv4CidrBlocks:   []string{"10.0.0.0/16"},
								},
							},
						},
					},
				},
			},
		},
	}
	b := backend.New(&mockReader{hw: hw})
	got, err := b.GetEC2Instance(context.Background(), "10.0.0.5")
	if err != nil {
		t.Fatalf("GetEC2Instance: %v", err)
	}

	iface, ok := got.Metadata.Network.Interfaces["02:aa:bb:cc:dd:ee"]
	if !ok {
		t.Fatalf("expected mac key in network.interfaces; got %+v", got.Metadata.Network)
	}
	if iface.InterfaceID == nil || *iface.InterfaceID != "eni-abc" {
		t.Errorf("InterfaceID = %v, want pointer to \"eni-abc\"", iface.InterfaceID)
	}
	if diff := cmp.Diff([]string{"10.0.0.5"}, iface.LocalIPv4s); diff != "" {
		t.Errorf("LocalIPv4s mismatch (-want +got):\n%s", diff)
	}
	if iface.SubnetIPv4CidrBlock == nil || *iface.SubnetIPv4CidrBlock != "10.0.0.0/24" {
		t.Errorf("SubnetIPv4CidrBlock = %v, want pointer to \"10.0.0.0/24\"", iface.SubnetIPv4CidrBlock)
	}
	if iface.DeviceNumber == nil || *iface.DeviceNumber != 0 {
		t.Errorf("DeviceNumber = %v, want pointer to 0", iface.DeviceNumber)
	}
}
```

Note: the existing `backend_test.go` is in `package backend` (internal) so it calls the unexported functions directly. Keep the new test in the same package. Check the current package declaration:

```bash
head -1 tootles/internal/backend/backend_test.go
```

If it's `package backend`, the call becomes simply `got, err := GetEC2Instance(...)` via a `Backend` instance. Read the existing test structure and match it exactly.

**Step 2: Run it — expect fail**

Run: `go test ./tootles/internal/backend/ -run TestGetEC2Instance_PassesThroughNetwork -v`

Expected: FAIL (the converter doesn't copy Network yet, so `got.Metadata.Network.Interfaces` is `nil`).

**Step 3: Implement in `toEC2Instance`**

Inside the `if hw.Spec.Metadata != nil && hw.Spec.Metadata.Instance != nil` block (currently ends around line 136), after the IP iteration, add:

```go
		if hw.Spec.Metadata.Instance.Network != nil &&
			hw.Spec.Metadata.Instance.Network.Interfaces != nil {
			ifaces := make(map[string]data.NetworkInterface, len(hw.Spec.Metadata.Instance.Network.Interfaces.Macs))
			for mac, src := range hw.Spec.Metadata.Instance.Network.Interfaces.Macs {
				if src == nil {
					continue
				}
				ifaces[strings.ToLower(mac)] = data.NetworkInterface(*src)
			}
			i.Metadata.Network.Interfaces = ifaces
		}
```

Since both structs now have identical field layouts (Hardware-side `MetadataInstanceNetworkInterface` and data-side `NetworkInterface` have the same ordered fields with matching types), a direct conversion works. If the Go compiler rejects it because the types are distinct, fall back to the explicit field-by-field copy (all fields are simple pointer/slice copies — no dereferences needed).

Add `"strings"` to the imports block if not already present.

**Step 4: Run test — expect pass**

Run: `go test ./tootles/internal/backend/ -run TestGetEC2Instance_PassesThroughNetwork -v`
Expected: PASS.

**Step 5: Run the full backend suite**

Run: `go test ./tootles/internal/backend/ -v`
Expected: all tests pass including the existing ones.

**Step 6: Commit**

```bash
git add tootles/internal/backend/backend.go tootles/internal/backend/backend_test.go
git commit -m "tootles: copy network metadata in toEC2Instance

MAC keys are lowercased during conversion so IMDS consumers see a
consistent format regardless of how Hardware resources are authored."
```

---

## Task 5: Write the network subtree handler (scaffolding)

**Files:**
- Create: `tootles/internal/frontend/ec2/network.go`
- Create: `tootles/internal/frontend/ec2/network_test.go`

This task builds the subtree handler incrementally with TDD. Each sub-step adds one layer of the tree.

### Step 5.1: Leaf accessor table

Create `tootles/internal/frontend/ec2/network.go`:

```go
package ec2

import (
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tinkerbell/tinkerbell/pkg/data"
	"github.com/tinkerbell/tinkerbell/tootles/internal/ginutil"
)

// networkInterfaceLeaves defines the ordered list of AWS-IMDS-compatible leaf
// fields served under /meta-data/network/interfaces/macs/{mac}/. Each entry
// produces the response body for a single GET. The bool return distinguishes
// "unset" (→ 404) from "set to zero value" (→ 200 with empty body).
var networkInterfaceLeaves = []struct {
	Name string
	Get  func(data.NetworkInterface) (string, bool)
}{
	{"device-number", func(n data.NetworkInterface) (string, bool) {
		if n.DeviceNumber == nil {
			return "", false
		}
		return strconv.FormatInt(*n.DeviceNumber, 10), true
	}},
	{"interface-id", func(n data.NetworkInterface) (string, bool) {
		if n.InterfaceID == nil {
			return "", false
		}
		return *n.InterfaceID, true
	}},
	{"ipv6s", func(n data.NetworkInterface) (string, bool) {
		if len(n.IPv6s) == 0 {
			return "", false
		}
		return strings.Join(n.IPv6s, "\n"), true
	}},
	{"local-hostname", func(n data.NetworkInterface) (string, bool) {
		if n.LocalHostname == nil {
			return "", false
		}
		return *n.LocalHostname, true
	}},
	{"local-ipv4s", func(n data.NetworkInterface) (string, bool) {
		if len(n.LocalIPv4s) == 0 {
			return "", false
		}
		return strings.Join(n.LocalIPv4s, "\n"), true
	}},
	{"mac", func(n data.NetworkInterface) (string, bool) {
		if n.Mac == nil {
			return "", false
		}
		return *n.Mac, true
	}},
	{"public-hostname", func(n data.NetworkInterface) (string, bool) {
		if n.PublicHostname == nil {
			return "", false
		}
		return *n.PublicHostname, true
	}},
	{"public-ipv4s", func(n data.NetworkInterface) (string, bool) {
		if len(n.PublicIPv4s) == 0 {
			return "", false
		}
		return strings.Join(n.PublicIPv4s, "\n"), true
	}},
	{"subnet-ipv4-cidr-block", func(n data.NetworkInterface) (string, bool) {
		if n.SubnetIPv4CidrBlock == nil {
			return "", false
		}
		return *n.SubnetIPv4CidrBlock, true
	}},
	{"subnet-ipv6-cidr-blocks", func(n data.NetworkInterface) (string, bool) {
		if len(n.SubnetIPv6CidrBlocks) == 0 {
			return "", false
		}
		return strings.Join(n.SubnetIPv6CidrBlocks, "\n"), true
	}},
	{"vpc-ipv4-cidr-blocks", func(n data.NetworkInterface) (string, bool) {
		if len(n.VpcIPv4CidrBlocks) == 0 {
			return "", false
		}
		return strings.Join(n.VpcIPv4CidrBlocks, "\n"), true
	}},
	{"vpc-ipv6-cidr-blocks", func(n data.NetworkInterface) (string, bool) {
		if len(n.VpcIPv6CidrBlocks) == 0 {
			return "", false
		}
		return strings.Join(n.VpcIPv6CidrBlocks, "\n"), true
	}},
}

// leafValue returns (value, true) if the named leaf is set, or ("", false) otherwise.
func leafValue(n data.NetworkInterface, name string) (string, bool) {
	for _, l := range networkInterfaceLeaves {
		if l.Name == name {
			return l.Get(n)
		}
	}
	return "", false
}

// leafListing returns a newline-joined listing of leaf names that are set on n.
// Output is sorted alphabetically to match AWS IMDS behaviour.
func leafListing(n data.NetworkInterface) string {
	var out []string
	for _, l := range networkInterfaceLeaves {
		if _, ok := l.Get(n); ok {
			out = append(out, l.Name)
		}
	}
	sort.Strings(out)
	return strings.Join(out, "\n")
}

// macListing returns a newline-joined listing of MAC keys from the network
// config, each with a trailing slash to signal they are directories in IMDS.
// Output is sorted alphabetically.
func macListing(net data.Network) string {
	keys := make([]string, 0, len(net.Interfaces))
	for mac := range net.Interfaces {
		keys = append(keys, mac+"/")
	}
	sort.Strings(keys)
	return strings.Join(keys, "\n")
}

// lookupInterface returns the interface for the given mac (case-insensitive match).
func lookupInterface(net data.Network, mac string) (data.NetworkInterface, bool) {
	iface, ok := net.Interfaces[strings.ToLower(mac)]
	return iface, ok
}
```

### Step 5.2: Unit tests for the helpers

Create `tootles/internal/frontend/ec2/network_test.go`:

```go
package ec2

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/tinkerbell/tinkerbell/pkg/data"
)

func TestLeafValue(t *testing.T) {
	ptr := func(s string) *string { return &s }
	dn := int64(3)
	n := data.NetworkInterface{
		Mac:                 ptr("02:aa:bb:cc:dd:ee"),
		LocalIPv4s:          []string{"10.0.0.5", "10.0.0.6"},
		SubnetIPv4CidrBlock: ptr("10.0.0.0/24"),
		DeviceNumber:        &dn,
	}
	cases := []struct {
		leaf     string
		wantVal  string
		wantOk   bool
	}{
		{"mac", "02:aa:bb:cc:dd:ee", true},
		{"local-ipv4s", "10.0.0.5\n10.0.0.6", true},
		{"subnet-ipv4-cidr-block", "10.0.0.0/24", true},
		{"device-number", "3", true},
		{"public-ipv4s", "", false},
		{"does-not-exist", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.leaf, func(t *testing.T) {
			got, ok := leafValue(n, tc.leaf)
			if got != tc.wantVal || ok != tc.wantOk {
				t.Errorf("leafValue(%s) = (%q, %v); want (%q, %v)",
					tc.leaf, got, ok, tc.wantVal, tc.wantOk)
			}
		})
	}
}

func TestLeafListing(t *testing.T) {
	ptr := func(s string) *string { return &s }
	n := data.NetworkInterface{
		Mac:        ptr("02:aa:bb:cc:dd:ee"),
		LocalIPv4s: []string{"10.0.0.5"},
	}
	got := leafListing(n)
	want := "local-ipv4s\nmac"
	if got != want {
		t.Errorf("leafListing = %q; want %q", got, want)
	}
}

func TestMacListing_SortedWithSlash(t *testing.T) {
	net := data.Network{
		Interfaces: map[string]data.NetworkInterface{
			"02:bb:bb:bb:bb:bb": {},
			"02:aa:aa:aa:aa:aa": {},
		},
	}
	got := macListing(net)
	want := "02:aa:aa:aa:aa:aa/\n02:bb:bb:bb:bb:bb/"
	if got != want {
		t.Errorf("macListing = %q; want %q", got, want)
	}
}

func TestLookupInterface_CaseInsensitive(t *testing.T) {
	ptr := func(s string) *string { return &s }
	net := data.Network{
		Interfaces: map[string]data.NetworkInterface{
			"02:aa:bb:cc:dd:ee": {Mac: ptr("02:aa:bb:cc:dd:ee")},
		},
	}
	for _, mac := range []string{"02:aa:bb:cc:dd:ee", "02:AA:BB:CC:DD:EE", "02:Aa:bB:Cc:Dd:Ee"} {
		if _, ok := lookupInterface(net, mac); !ok {
			t.Errorf("lookupInterface(%q) returned not-found", mac)
		}
	}
	if _, ok := lookupInterface(net, "02:ff:ff:ff:ff:ff"); ok {
		t.Errorf("lookupInterface(unknown) should be not-found")
	}
	_ = cmp.Diff // keep import
}
```

Run: `go test ./tootles/internal/frontend/ec2/ -run 'TestLeafValue|TestLeafListing|TestMacListing|TestLookupInterface' -v`
Expected: all pass.

**Commit:**

```bash
git add tootles/internal/frontend/ec2/network.go tootles/internal/frontend/ec2/network_test.go
git commit -m "tootles/ec2: add network subtree helpers

Lookup, listing, and leaf-value helpers backing the per-interface
IMDS tree. No HTTP handlers yet — those come in the next commit."
```

### Step 5.3: HTTP handler for the network subtree

Append to `network.go`:

```go
// configureNetworkRoutes registers all routes under /meta-data/network/.
// The subtree has three fixed-depth levels plus two dynamic per-MAC levels:
//
//	/meta-data/network                                        → "interfaces/"
//	/meta-data/network/interfaces                             → "macs/"
//	/meta-data/network/interfaces/macs                        → list of "{mac}/"
//	/meta-data/network/interfaces/macs/:mac                   → leaf name listing
//	/meta-data/network/interfaces/macs/:mac/:leaf             → leaf value
//
// The handler relies on f.getInstanceViaIP / getInstanceViaInstanceID to resolve
// the instance. viaInstanceID controls which lookup is used.
func (f Frontend) configureNetworkRoutes(router ginutil.TrailingSlashRouteHelper, viaInstanceID bool) {
	const base = "/meta-data/network"

	resolve := func(ctx *gin.Context) (data.Ec2Instance, error) {
		if viaInstanceID {
			return f.getInstanceViaInstanceID(ctx)
		}
		return f.getInstanceViaIP(ctx, ctx.Request)
	}

	// /meta-data/network → "interfaces/"
	router.GET(base, func(ctx *gin.Context) {
		ctx.String(http.StatusOK, "interfaces/")
	})

	// /meta-data/network/interfaces → "macs/"
	router.GET(base+"/interfaces", func(ctx *gin.Context) {
		ctx.String(http.StatusOK, "macs/")
	})

	// /meta-data/network/interfaces/macs → listing of MACs
	router.GET(base+"/interfaces/macs", func(ctx *gin.Context) {
		instance, err := resolve(ctx)
		if err != nil {
			f.writeInstanceDataOrErrToHTTP(ctx, err, "")
			return
		}
		ctx.String(http.StatusOK, macListing(instance.Metadata.Network))
	})

	// /meta-data/network/interfaces/macs/:mac → leaf listing
	router.GET(base+"/interfaces/macs/:mac", func(ctx *gin.Context) {
		instance, err := resolve(ctx)
		if err != nil {
			f.writeInstanceDataOrErrToHTTP(ctx, err, "")
			return
		}
		iface, ok := lookupInterface(instance.Metadata.Network, ctx.Param("mac"))
		if !ok {
			ctx.AbortWithStatus(http.StatusNotFound)
			return
		}
		ctx.String(http.StatusOK, leafListing(iface))
	})

	// /meta-data/network/interfaces/macs/:mac/:leaf → leaf value
	router.GET(base+"/interfaces/macs/:mac/:leaf", func(ctx *gin.Context) {
		instance, err := resolve(ctx)
		if err != nil {
			f.writeInstanceDataOrErrToHTTP(ctx, err, "")
			return
		}
		iface, ok := lookupInterface(instance.Metadata.Network, ctx.Param("mac"))
		if !ok {
			ctx.AbortWithStatus(http.StatusNotFound)
			return
		}
		value, ok := leafValue(iface, ctx.Param("leaf"))
		if !ok {
			ctx.AbortWithStatus(http.StatusNotFound)
			return
		}
		ctx.String(http.StatusOK, value)
	})
}
```

Run: `go build ./tootles/internal/frontend/ec2/`
Expected: no errors.

**Commit:**

```bash
git add tootles/internal/frontend/ec2/network.go
git commit -m "tootles/ec2: add network subtree HTTP handler"
```

---

## Task 6: Wire `configureNetworkRoutes` into `Frontend.Configure`

**Files:**
- Modify: `tootles/internal/frontend/ec2/frontend.go`
- Modify: `tootles/internal/frontend/ec2/frontend_test.go` (update `/meta-data` static listing expectation)

### Step 6.1: Wire in Configure

In `Frontend.Configure`, after the existing `for _, r := range dataRoutes { ... }` loop and before the `staticRoutes.Build()` call, add:

```go
	// Register network subtree routes (dynamic per-MAC paths; not representable
	// in the static route builder). Also register a placeholder leaf with the
	// staticRoutes builder so the /meta-data listing includes "network/".
	staticRoutes.FromEndpoint("/meta-data/network/interfaces")
	f.configureNetworkRoutes(v20090404, false)
	if f.instanceEndpoint {
		f.configureNetworkRoutes(v20090404viaInstanceID, true)
	}
```

Then immediately after `staticRoutes.Build()` is consumed, filter out any static route whose endpoint is under `/meta-data/network` so the custom dynamic handlers aren't shadowed/duplicated. Look at the existing code:

```go
for _, r := range staticRoutes.Build() {
    staticEndpointBinder(v20090404, r.Endpoint, r.Children)
    if f.instanceEndpoint {
        staticEndpointBinder(v20090404viaInstanceID, r.Endpoint, r.Children)
    }
}
```

Replace with:

```go
for _, r := range staticRoutes.Build() {
    if strings.HasPrefix(r.Endpoint, "/meta-data/network") {
        // configureNetworkRoutes already registered dynamic handlers for this subtree.
        continue
    }
    staticEndpointBinder(v20090404, r.Endpoint, r.Children)
    if f.instanceEndpoint {
        staticEndpointBinder(v20090404viaInstanceID, r.Endpoint, r.Children)
    }
}
```

`strings` is already imported.

### Step 6.2: Update the existing `/meta-data` static listing test

In `frontend_test.go`, the `Metadata` case (currently around lines 347–360):

```go
{
    Name:     "Metadata",
    Endpoint: "/2009-04-04/meta-data",
    Expect: `facility
hostname
instance-id
iqn
local-hostname
local-ipv4
operating-system/
plan
public-ipv4
public-ipv6
public-keys
tags`,
},
```

Change `Expect` to include `network/` in alphabetical order (between `local-ipv4` and `operating-system/`):

```go
    Expect: `facility
hostname
instance-id
iqn
local-hostname
local-ipv4
network/
operating-system/
plan
public-ipv4
public-ipv6
public-keys
tags`,
```

### Step 6.3: Run the existing frontend test suite

Run: `go test ./tootles/internal/frontend/ec2/ -v -run '^TestFrontend'`

Expected: all existing tests pass with the updated `Metadata` expectation.

**Commit:**

```bash
git add tootles/internal/frontend/ec2/frontend.go tootles/internal/frontend/ec2/frontend_test.go
git commit -m "tootles/ec2: wire network subtree into Frontend.Configure

Adds network/ to the /meta-data listing and registers the dynamic
handlers for both the IP-based and instanceID-based route groups.
Skips static-route binding for endpoints under /meta-data/network/
because those handlers are dynamic."
```

---

## Task 7: End-to-end HTTP test for the full tree

**Files:**
- Modify: `tootles/internal/frontend/ec2/frontend_test.go` (new test `TestFrontendNetworkEndpoints`)

### Step 1: Write the failing test

Append to `frontend_test.go`:

```go
func TestFrontendNetworkEndpoints(t *testing.T) {
	ptr := func(s string) *string { return &s }
	deviceNumber := int64(0)
	instance := data.Ec2Instance{
		Metadata: data.Metadata{
			Network: data.Network{
				Interfaces: map[string]data.NetworkInterface{
					"02:aa:bb:cc:dd:ee": {
						DeviceNumber:        &deviceNumber,
						InterfaceID:         ptr("eni-abc"),
						Mac:                 ptr("02:aa:bb:cc:dd:ee"),
						LocalIPv4s:          []string{"10.0.0.5", "10.0.0.6"},
						SubnetIPv4CidrBlock: ptr("10.0.0.0/24"),
						VpcIPv4CidrBlocks:   []string{"10.0.0.0/16"},
					},
					"02:ff:ff:ff:ff:ff": {
						Mac:        ptr("02:ff:ff:ff:ff:ff"),
						LocalIPv4s: []string{"10.0.1.5"},
					},
				},
			},
		},
	}

	cases := []struct {
		Name     string
		Endpoint string
		Expect   string
		Status   int // 0 means 200 OK
	}{
		{
			Name:     "NetworkRoot",
			Endpoint: "/2009-04-04/meta-data/network",
			Expect:   "interfaces/",
		},
		{
			Name:     "Interfaces",
			Endpoint: "/2009-04-04/meta-data/network/interfaces",
			Expect:   "macs/",
		},
		{
			Name:     "MacsListing",
			Endpoint: "/2009-04-04/meta-data/network/interfaces/macs",
			Expect:   "02:aa:bb:cc:dd:ee/\n02:ff:ff:ff:ff:ff/",
		},
		{
			Name:     "LeafListing",
			Endpoint: "/2009-04-04/meta-data/network/interfaces/macs/02:aa:bb:cc:dd:ee",
			Expect:   "device-number\ninterface-id\nlocal-ipv4s\nmac\nsubnet-ipv4-cidr-block\nvpc-ipv4-cidr-blocks",
		},
		{
			Name:     "LeafMac",
			Endpoint: "/2009-04-04/meta-data/network/interfaces/macs/02:aa:bb:cc:dd:ee/mac",
			Expect:   "02:aa:bb:cc:dd:ee",
		},
		{
			Name:     "LeafLocalIPv4s",
			Endpoint: "/2009-04-04/meta-data/network/interfaces/macs/02:aa:bb:cc:dd:ee/local-ipv4s",
			Expect:   "10.0.0.5\n10.0.0.6",
		},
		{
			Name:     "LeafDeviceNumberZero",
			Endpoint: "/2009-04-04/meta-data/network/interfaces/macs/02:aa:bb:cc:dd:ee/device-number",
			Expect:   "0",
		},
		{
			Name:     "LeafUppercaseMacNormalised",
			Endpoint: "/2009-04-04/meta-data/network/interfaces/macs/02:AA:BB:CC:DD:EE/mac",
			Expect:   "02:aa:bb:cc:dd:ee",
		},
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			client := ec2.NewMockClient(ctrl)
			client.EXPECT().
				GetEC2Instance(gomock.Any(), gomock.Any()).
				Return(instance, nil).
				Times(2)

			router := gin.New()
			fe := ec2.New(client, false)
			fe.Configure(router)

			validate(t, router, tc.Endpoint, tc.Expect)
			validate(t, router, tc.Endpoint+"/", tc.Expect)
		})
	}
}

func TestFrontendNetworkUnknownMac(t *testing.T) {
	ptr := func(s string) *string { return &s }
	instance := data.Ec2Instance{
		Metadata: data.Metadata{
			Network: data.Network{
				Interfaces: map[string]data.NetworkInterface{
					"02:aa:bb:cc:dd:ee": {Mac: ptr("02:aa:bb:cc:dd:ee")},
				},
			},
		},
	}
	ctrl := gomock.NewController(t)
	client := ec2.NewMockClient(ctrl)
	client.EXPECT().
		GetEC2Instance(gomock.Any(), gomock.Any()).
		Return(instance, nil).
		AnyTimes()

	router := gin.New()
	fe := ec2.New(client, false)
	fe.Configure(router)

	for _, endpoint := range []string{
		"/2009-04-04/meta-data/network/interfaces/macs/02:ff:ff:ff:ff:ff",
		"/2009-04-04/meta-data/network/interfaces/macs/02:aa:bb:cc:dd:ee/public-ipv4s",
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", endpoint, nil)
		r.RemoteAddr = "10.10.10.10:0"
		router.ServeHTTP(w, r)
		if w.Code != http.StatusNotFound {
			t.Errorf("%s: expected 404, got %d", endpoint, w.Code)
		}
	}
}
```

### Step 2: Run — expect pass (implementation from Task 5+6 should satisfy it)

Run: `go test ./tootles/internal/frontend/ec2/ -v -run 'TestFrontendNetworkEndpoints|TestFrontendNetworkUnknownMac'`
Expected: PASS.

If failures, adjust handler behaviour in `network.go`. Do not adjust the test to match buggy behaviour.

### Step 3: Run the full test suite

Run: `go test ./... 2>&1 | tail -30`
Expected: all pass.

### Step 4: Commit

```bash
git add tootles/internal/frontend/ec2/frontend_test.go
git commit -m "test: end-to-end coverage for EC2 network metadata tree

Covers directory listings at each level, scalar leaves, list leaves
served as newline-joined text, zero-valued device-number served as
\"0\" (not 404), case-insensitive MAC lookup, and 404 on unknown
MAC or unset leaf."
```

---

## Task 8: Final verification + push

### Step 1: Full build, vet, test

Run: `go build ./... && go vet ./... && go test ./... 2>&1 | tail -20`
Expected: no failures.

### Step 2: Lint (if golangci-lint is configured)

Run: `make lint 2>&1 | tail -20`
Expected: no new issues.

### Step 3: Check git log — all commits on `feat/metadata-network`

Run: `git log --oneline feat/metadata-raid-passthrough..HEAD`
Expected: 8 commits (Task 0 through Task 7) plus the design doc commit.

### Step 4: Push the branch

Run: `git push -u origin feat/metadata-network`

### Step 5: Open a PR (user's choice — not automated)

User decides whether to open against their fork's main or upstream `tinkerbell/tinkerbell`.

---

## YAGNI reminders (do NOT implement unless explicitly asked)

- No MAC-format validation on the CRD.
- No CIDR-format validation.
- No auto-derivation from `Hardware.Spec.Interfaces[].DHCP`.
- No HackInstance mirror — EC2 frontend only.
- No separate `/2011-01-01/` API version prefix.
- No `vpc-id`, `subnet-id`, `owner-id`, `security-groups` fields.
- No docs pages in `docs/technical/` unless the user asks.

## Skills to consult during execution

- `superpowers:test-driven-development` — writing the test first, making it fail, making it pass.
- `superpowers:verification-before-completion` — run the named command, see PASS, before claiming a task is done.
