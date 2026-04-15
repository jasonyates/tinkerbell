package ec2

import (
	"testing"

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
		leaf    string
		wantVal string
		wantOk  bool
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
	net := data.InstanceNetwork{
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
	net := data.InstanceNetwork{
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
}
