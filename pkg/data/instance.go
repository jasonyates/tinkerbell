package data

// Ec2Instance is a struct that contains the hardware data exposed from the EC2 API endpoints. For
// an explanation of the endpoints refer to the AWS EC2 Ec2Instance Metadata documentation.
//
//	https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/instancedata-data-categories.html
//
// Note not all AWS EC2 Ec2Instance Metadata categories are supported as some are not applicable.
// Deviations from the AWS EC2 Ec2Instance Metadata should be documented here.
type Ec2Instance struct {
	Userdata string
	Metadata Metadata
}

// Metadata is a part of Instance.
type Metadata struct {
	InstanceID      string
	Hostname        string
	LocalHostname   string
	IQN             string
	Plan            string
	Facility        string
	Tags            []string
	PublicKeys      []string
	PublicIPv4      string
	PublicIPv6      string
	LocalIPv4       string
	OperatingSystem OperatingSystem
	Network         InstanceNetwork
}

// OperatingSystem is part of Metadata.
type OperatingSystem struct {
	Slug              string
	Distro            string
	Version           string
	ImageTag          string
	LicenseActivation LicenseActivation
}

// LicenseActivation is part of OperatingSystem.
type LicenseActivation struct {
	State string
}

// InstanceNetwork is part of Metadata. Mirrors the AWS EC2 metadata
// network/ tree. Named InstanceNetwork (rather than Network) to avoid
// collision with the agent-attribute Network type in attributes.go.
type InstanceNetwork struct {
	// Interfaces is keyed by lowercase, colon-separated MAC address.
	Interfaces map[string]NetworkInterface
}

// NetworkInterface holds the per-NIC fields served under
// /meta-data/network/interfaces/macs/{mac}/. Scalar pointer fields
// distinguish "not set" (served as HTTP 404 by the IMDS frontend) from
// "set to empty string" (served as 200 with empty body). Slice fields
// use nil vs non-nil for the same distinction.
//
// Field order matches v1alpha1.MetadataInstanceNetworkInterface so that
// the backend converter can use a direct Go type conversion.
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

// Instance is a representation of the instance metadata. Its based on the rooitio hub action
// and should have just enough information for it to work.
type HackInstance struct {
	Metadata struct {
		Instance struct {
			Storage struct {
				Disks []struct {
					Device     string `json:"device"`
					Partitions []struct {
						Label  string `json:"label"`
						Number int    `json:"number"`
						Size   uint64 `json:"size"`
					} `json:"partitions"`
					WipeTable bool `json:"wipe_table"`
				} `json:"disks"`
				Raid []struct {
					Name    string   `json:"name"`
					Level   string   `json:"level"`
					Devices []string `json:"devices"`
					Spare   []string `json:"spare,omitempty"`
				} `json:"raid"`
				Filesystems []struct {
					Mount struct {
						Create struct {
							Options []string `json:"options"`
						} `json:"create"`
						Device string `json:"device"`
						Format string `json:"format"`
						Point  string `json:"point"`
					} `json:"mount"`
				} `json:"filesystems"`
			} `json:"storage"`
		} `json:"instance"`
	} `json:"metadata"`
}
