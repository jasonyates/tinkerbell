package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/go-cmp/cmp"
	v1alpha1 "github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	"github.com/tinkerbell/tinkerbell/pkg/data"
	"github.com/tinkerbell/tinkerbell/tootles/internal/frontend/ec2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Compile-time assertion: the Hardware CR per-interface type must stay
// layout-identical to data.NetworkInterface so toEC2Instance can use a
// direct Go type conversion. Reordering or re-typing a field on either
// side fails this at build time.
var _ = data.NetworkInterface(v1alpha1.MetadataInstanceNetworkInterface{})

type mockReader struct {
	hw  *v1alpha1.Hardware
	err error
}

func (m *mockReader) FilterHardware(_ context.Context, _ data.HardwareFilter) (*v1alpha1.Hardware, error) {
	return m.hw, m.err
}

// notFoundError satisfies the notFounder interface and the apierrors.APIStatus interface.
type notFoundError struct {
	msg string
}

func (e notFoundError) Error() string  { return e.msg }
func (e notFoundError) NotFound() bool { return true }
func (e notFoundError) Status() metav1.Status {
	return metav1.Status{
		Reason: metav1.StatusReasonNotFound,
		Code:   http.StatusNotFound,
	}
}

func TestGetEC2Instance(t *testing.T) {
	userData := "my-user-data"
	tests := map[string]struct {
		reader  *mockReader
		ip      string
		want    data.Ec2Instance
		wantErr error
	}{
		"success with full metadata": {
			reader: &mockReader{
				hw: &v1alpha1.Hardware{
					Spec: v1alpha1.HardwareSpec{
						UserData: &userData,
						Metadata: &v1alpha1.HardwareMetadata{
							Instance: &v1alpha1.MetadataInstance{
								ID:       "inst-123",
								Hostname: "my-host",
								Tags:     []string{"tag1", "tag2"},
								OperatingSystem: &v1alpha1.MetadataInstanceOperatingSystem{
									Slug:     "ubuntu",
									Distro:   "ubuntu",
									Version:  "20.04",
									ImageTag: "v1",
								},
								Ips: []*v1alpha1.MetadataInstanceIP{
									{Address: "1.2.3.4", Family: 4, Public: true},
									{Address: "10.0.0.1", Family: 4, Public: false},
									{Address: "2001:db8::1", Family: 6, Public: true},
								},
							},
							Facility: &v1alpha1.MetadataFacility{
								PlanSlug:     "c3.small.x86",
								FacilityCode: "sjc1",
							},
						},
					},
				},
			},
			ip: "10.0.0.1",
			want: data.Ec2Instance{
				Userdata: "my-user-data",
				Metadata: data.Metadata{
					InstanceID:    "inst-123",
					Hostname:      "my-host",
					LocalHostname: "my-host",
					Tags:          []string{"tag1", "tag2"},
					PublicIPv4:    "1.2.3.4",
					LocalIPv4:     "10.0.0.1",
					PublicIPv6:    "2001:db8::1",
					Plan:          "c3.small.x86",
					Facility:      "sjc1",
					OperatingSystem: data.OperatingSystem{
						Slug:     "ubuntu",
						Distro:   "ubuntu",
						Version:  "20.04",
						ImageTag: "v1",
					},
				},
			},
		},
		"success with nil metadata": {
			reader: &mockReader{
				hw: &v1alpha1.Hardware{},
			},
			ip:   "10.0.0.1",
			want: data.Ec2Instance{},
		},
		"not found error wraps as ec2.ErrInstanceNotFound": {
			reader: &mockReader{
				err: notFoundError{msg: "hardware not found: 10.0.0.1"},
			},
			ip:      "10.0.0.1",
			wantErr: ec2.ErrInstanceNotFound,
		},
		"generic error returned as-is": {
			reader: &mockReader{
				err: fmt.Errorf("connection refused"),
			},
			ip:      "10.0.0.1",
			wantErr: fmt.Errorf("connection refused"),
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			b := New(tt.reader)
			got, err := b.GetEC2Instance(context.Background(), tt.ip)

			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if errors.Is(tt.wantErr, ec2.ErrInstanceNotFound) {
					if !errors.Is(err, ec2.ErrInstanceNotFound) {
						t.Fatalf("expected error wrapping ec2.ErrInstanceNotFound, got: %v", err)
					}
					return
				}
				if err.Error() != tt.wantErr.Error() {
					t.Fatalf("expected error %q, got %q", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Fatalf("ec2 instance mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestGetEC2InstanceByInstanceID(t *testing.T) {
	tests := map[string]struct {
		reader  *mockReader
		want    data.Ec2Instance
		wantErr error
	}{
		"success": {
			reader: &mockReader{
				hw: &v1alpha1.Hardware{
					Spec: v1alpha1.HardwareSpec{
						Metadata: &v1alpha1.HardwareMetadata{
							Instance: &v1alpha1.MetadataInstance{
								ID:       "inst-456",
								Hostname: "host-456",
							},
						},
					},
				},
			},
			want: data.Ec2Instance{
				Metadata: data.Metadata{
					InstanceID:    "inst-456",
					Hostname:      "host-456",
					LocalHostname: "host-456",
				},
			},
		},
		"not found": {
			reader:  &mockReader{err: notFoundError{msg: "not found"}},
			wantErr: ec2.ErrInstanceNotFound,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			b := New(tt.reader)
			got, err := b.GetEC2InstanceByInstanceID(context.Background(), "inst-456")

			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if errors.Is(tt.wantErr, ec2.ErrInstanceNotFound) && !errors.Is(err, ec2.ErrInstanceNotFound) {
					t.Fatalf("expected ec2.ErrInstanceNotFound, got: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Fatalf("ec2 instance mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestGetHackInstance(t *testing.T) {
	tests := map[string]struct {
		reader  *mockReader
		want    data.HackInstance
		wantErr bool
	}{
		"success with storage": {
			reader: &mockReader{
				hw: &v1alpha1.Hardware{
					Spec: v1alpha1.HardwareSpec{
						Metadata: &v1alpha1.HardwareMetadata{
							Instance: &v1alpha1.MetadataInstance{
								Storage: &v1alpha1.MetadataInstanceStorage{
									Disks: []*v1alpha1.MetadataInstanceStorageDisk{
										{
											Device:    "/dev/sda",
											WipeTable: true,
											Partitions: []*v1alpha1.MetadataInstanceStorageDiskPartition{
												{Label: "root", Number: 1, Size: 1000000},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		"error from reader": {
			reader:  &mockReader{err: fmt.Errorf("fail")},
			wantErr: true,
		},
		"empty hardware": {
			reader: &mockReader{hw: &v1alpha1.Hardware{}},
			want:   data.HackInstance{},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			b := New(tt.reader)
			_, err := b.GetHackInstance(context.Background(), "10.0.0.1")

			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestToEC2Instance(t *testing.T) {
	userData := "cloud-init data"
	tests := map[string]struct {
		hw   v1alpha1.Hardware
		want data.Ec2Instance
	}{
		"nil metadata": {
			hw:   v1alpha1.Hardware{},
			want: data.Ec2Instance{},
		},
		"nil instance": {
			hw: v1alpha1.Hardware{
				Spec: v1alpha1.HardwareSpec{
					Metadata: &v1alpha1.HardwareMetadata{},
				},
			},
			want: data.Ec2Instance{},
		},
		"facility only": {
			hw: v1alpha1.Hardware{
				Spec: v1alpha1.HardwareSpec{
					Metadata: &v1alpha1.HardwareMetadata{
						Facility: &v1alpha1.MetadataFacility{
							PlanSlug:     "plan-a",
							FacilityCode: "dc1",
						},
					},
				},
			},
			want: data.Ec2Instance{
				Metadata: data.Metadata{
					Plan:     "plan-a",
					Facility: "dc1",
				},
			},
		},
		"userdata": {
			hw: v1alpha1.Hardware{
				Spec: v1alpha1.HardwareSpec{
					UserData: &userData,
				},
			},
			want: data.Ec2Instance{
				Userdata: "cloud-init data",
			},
		},
		"first matching IPs chosen": {
			hw: v1alpha1.Hardware{
				Spec: v1alpha1.HardwareSpec{
					Metadata: &v1alpha1.HardwareMetadata{
						Instance: &v1alpha1.MetadataInstance{
							Ips: []*v1alpha1.MetadataInstanceIP{
								{Address: "pub4-first", Family: 4, Public: true},
								{Address: "pub4-second", Family: 4, Public: true},
								{Address: "priv4-first", Family: 4, Public: false},
								{Address: "priv4-second", Family: 4, Public: false},
								{Address: "pub6-first", Family: 6},
								{Address: "pub6-second", Family: 6},
							},
						},
					},
				},
			},
			want: data.Ec2Instance{
				Metadata: data.Metadata{
					PublicIPv4: "pub4-first",
					LocalIPv4:  "priv4-first",
					PublicIPv6: "pub6-first",
				},
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := toEC2Instance(tt.hw)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Fatalf("(-want +got):\n%s", diff)
			}
		})
	}
}

func TestGetHackInstance_PassesThroughRAID(t *testing.T) {
	hw := &v1alpha1.Hardware{
		Spec: v1alpha1.HardwareSpec{
			Metadata: &v1alpha1.HardwareMetadata{
				Instance: &v1alpha1.MetadataInstance{
					Storage: &v1alpha1.MetadataInstanceStorage{
						Disks: []*v1alpha1.MetadataInstanceStorageDisk{
							{
								Device:    "/dev/sda",
								WipeTable: true,
								Partitions: []*v1alpha1.MetadataInstanceStorageDiskPartition{
									{Label: "root", Number: 1, Size: 1000000},
								},
							},
						},
						Raid: []*v1alpha1.MetadataInstanceStorageRAID{
							{
								Name:    "/dev/md0",
								Level:   "1",
								Devices: []string{"/dev/sda2", "/dev/sdb2"},
							},
						},
						Filesystems: []*v1alpha1.MetadataInstanceStorageFilesystem{
							{
								Mount: &v1alpha1.MetadataInstanceStorageMount{
									Device: "/dev/md0",
									Format: "ext4",
									Point:  "/",
								},
							},
						},
					},
				},
			},
		},
	}

	b := New(&mockReader{hw: hw})
	got, err := b.GetHackInstance(context.Background(), "1.2.3.4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	marshalled, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("failed to marshal HackInstance: %v", err)
	}

	var parsed struct {
		Metadata struct {
			Instance struct {
				Storage struct {
					Raid []struct {
						Name    string   `json:"name"`
						Level   string   `json:"level"`
						Devices []string `json:"devices"`
					} `json:"raid"`
				} `json:"storage"`
			} `json:"instance"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(marshalled, &parsed); err != nil {
		t.Fatalf("failed to unmarshal HackInstance JSON: %v", err)
	}

	raid := parsed.Metadata.Instance.Storage.Raid
	if len(raid) != 1 {
		t.Fatalf("expected exactly 1 raid entry, got %d", len(raid))
	}
	if raid[0].Name != "/dev/md0" {
		t.Errorf("raid name: got %q, want %q", raid[0].Name, "/dev/md0")
	}
	if raid[0].Level != "1" {
		t.Errorf("raid level: got %q, want %q", raid[0].Level, "1")
	}
	if diff := cmp.Diff([]string{"/dev/sda2", "/dev/sdb2"}, raid[0].Devices); diff != "" {
		t.Errorf("raid devices mismatch (-want +got):\n%s", diff)
	}
}

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
	b := New(&mockReader{hw: hw})
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

func TestGetEC2Instance_LowercasesMACKey(t *testing.T) {
	ptr := func(s string) *string { return &s }
	hw := &v1alpha1.Hardware{
		Spec: v1alpha1.HardwareSpec{
			Metadata: &v1alpha1.HardwareMetadata{
				Instance: &v1alpha1.MetadataInstance{
					Network: &v1alpha1.MetadataInstanceNetwork{
						Interfaces: &v1alpha1.MetadataInstanceNetworkInterfaces{
							Macs: map[string]*v1alpha1.MetadataInstanceNetworkInterface{
								"02:AA:BB:CC:DD:EE": {
									InterfaceID: ptr("eni-upper"),
									Mac:         ptr("02:AA:BB:CC:DD:EE"),
								},
							},
						},
					},
				},
			},
		},
	}
	b := New(&mockReader{hw: hw})
	got, err := b.GetEC2Instance(context.Background(), "10.0.0.5")
	if err != nil {
		t.Fatalf("GetEC2Instance: %v", err)
	}

	if _, ok := got.Metadata.Network.Interfaces["02:AA:BB:CC:DD:EE"]; ok {
		t.Errorf("uppercase MAC key must not appear in output; got %+v", got.Metadata.Network.Interfaces)
	}
	iface, ok := got.Metadata.Network.Interfaces["02:aa:bb:cc:dd:ee"]
	if !ok {
		t.Fatalf("expected lowercased mac key in network.interfaces; got %+v", got.Metadata.Network.Interfaces)
	}
	if iface.InterfaceID == nil || *iface.InterfaceID != "eni-upper" {
		t.Errorf("InterfaceID = %v, want pointer to \"eni-upper\"", iface.InterfaceID)
	}
}

func TestGetEC2Instance_SkipsNilNetworkInterface(t *testing.T) {
	hw := &v1alpha1.Hardware{
		Spec: v1alpha1.HardwareSpec{
			Metadata: &v1alpha1.HardwareMetadata{
				Instance: &v1alpha1.MetadataInstance{
					Network: &v1alpha1.MetadataInstanceNetwork{
						Interfaces: &v1alpha1.MetadataInstanceNetworkInterfaces{
							Macs: map[string]*v1alpha1.MetadataInstanceNetworkInterface{
								"02:aa:bb:cc:dd:ee": nil,
							},
						},
					},
				},
			},
		},
	}
	b := New(&mockReader{hw: hw})
	got, err := b.GetEC2Instance(context.Background(), "10.0.0.5")
	if err != nil {
		t.Fatalf("GetEC2Instance: %v", err)
	}

	if _, ok := got.Metadata.Network.Interfaces["02:aa:bb:cc:dd:ee"]; ok {
		t.Errorf("nil source interface must be skipped, but key present in output; got %+v", got.Metadata.Network.Interfaces)
	}
}

func TestIsNotFound(t *testing.T) {
	tests := map[string]struct {
		err  error
		want bool
	}{
		"nil error":     {err: nil, want: false},
		"generic error": {err: fmt.Errorf("oops"), want: false},
		"not found error": {
			err:  notFoundError{msg: "gone"},
			want: true,
		},
		"wrapped not found": {
			err:  fmt.Errorf("wrap: %w", notFoundError{msg: "gone"}),
			want: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := isNotFound(tt.err); got != tt.want {
				t.Fatalf("isNotFound() = %v, want %v", got, tt.want)
			}
		})
	}
}
