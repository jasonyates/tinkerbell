package kube

import (
	"encoding/json"
	"testing"

	v1alpha1 "github.com/tinkerbell/tinkerbell/pkg/api/v1alpha1/tinkerbell"
)

func TestToHackInstance_PassesThroughRAID(t *testing.T) {
	hw := v1alpha1.Hardware{
		Spec: v1alpha1.HardwareSpec{
			Metadata: &v1alpha1.HardwareMetadata{
				Instance: &v1alpha1.MetadataInstance{
					Storage: &v1alpha1.MetadataInstanceStorage{
						Disks: []*v1alpha1.MetadataInstanceStorageDisk{
							{Device: "/dev/sda", WipeTable: true},
						},
						Raid: []*v1alpha1.MetadataInstanceStorageRAID{
							{
								Name:    "/dev/md0",
								Level:   "1",
								Devices: []string{"/dev/sda2", "/dev/sdb2"},
							},
						},
						Filesystems: []*v1alpha1.MetadataInstanceStorageFilesystem{
							{Mount: &v1alpha1.MetadataInstanceStorageMount{
								Device: "/dev/md0", Format: "ext4", Point: "/",
							}},
						},
					},
				},
			},
		},
	}

	hi, err := toHackInstance(hw)
	if err != nil {
		t.Fatalf("toHackInstance: %v", err)
	}

	out, err := json.Marshal(hi)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var parsed struct {
		Metadata struct {
			Instance struct {
				Storage struct {
					RAID []struct {
						Name    string   `json:"name"`
						Level   string   `json:"level"`
						Devices []string `json:"devices"`
					} `json:"raid"`
				} `json:"storage"`
			} `json:"instance"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("reparse: %v", err)
	}

	got := parsed.Metadata.Instance.Storage.RAID
	if len(got) != 1 {
		t.Fatalf("want 1 raid entry in HackInstance JSON, got %d; JSON=%s", len(got), out)
	}
	if got[0].Name != "/dev/md0" || got[0].Level != "1" {
		t.Errorf("raid[0] = %+v, want name=/dev/md0 level=1", got[0])
	}
	if len(got[0].Devices) != 2 || got[0].Devices[0] != "/dev/sda2" || got[0].Devices[1] != "/dev/sdb2" {
		t.Errorf("raid[0].devices = %v, want [/dev/sda2 /dev/sdb2]", got[0].Devices)
	}
}
