package openstack

import (
	"regexp"
	"time"

	"github.com/ovh/cds/engine/service"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/flavors"

	hatcheryCommon "github.com/ovh/cds/engine/hatchery"
)

// HatcheryConfiguration is the configuration for hatchery
type HatcheryConfiguration struct {
	service.HatcheryCommonConfiguration `mapstructure:"commonConfiguration" toml:"commonConfiguration" json:"commonConfiguration"`

	// Tenant openstack-tenant
	Tenant string `mapstructure:"tenant" toml:"tenant" default:"" commented:"false" comment:"Openstack Tenant, generally value of $OS_TENANT_NAME, v2 auth only" json:"tenant,omitempty"`

	// Domain openstack-domain
	Domain string `mapstructure:"domain" toml:"domain" default:"" commented:"true" comment:"Openstack Domain, generally value of $OS_DOMAIN_NAME, v3 auth only" json:"domain,omitempty"`

	// User  openstack-user
	User string `mapstructure:"user" toml:"user" default:"" commented:"false" comment:"Openstack User" json:"user"`

	// Address  openstack-auth-endpoint
	Address string `mapstructure:"address" toml:"address" default:"https://auth.cloud.ovh.net/v3.0" commented:"false" comment:"Opentack Auth Endpoint" json:"address"`

	// Password openstack-password
	Password string `mapstructure:"password" toml:"password" default:"" commented:"false" comment:"Openstack Password" json:"-"`

	// Region openstack-region
	Region string `mapstructure:"region" toml:"region" default:"" commented:"false" comment:"Openstack Region" json:"region"`

	// NetworkString openstack-network
	NetworkString string `mapstructure:"networkString" toml:"networkString" default:"Ext-Net" commented:"false" comment:"Hatchery will use this Network to spawn CDS Worker (Virtual Machine)." json:"networkString,omitempty"`

	// IPRange IP Range
	IPRange string `mapstructure:"iprange" toml:"iprange" default:"" commented:"false" comment:"Facultative. IP Range for spawned workers. \n Format: a.a.a.a/b,c.c.c.c/e \n Hatchery will use an IP from this range to create Virtual Machine (Fixed IP Attribute).\nIf not set, it will get an address from the neutron service" json:"iprange,omitempty"`

	// DisableCreateImage if true: hatchery does not create openstack image when a worker model is updated
	DisableCreateImage bool `mapstructure:"disableCreateImage" toml:"disableCreateImage" default:"false" commented:"false" comment:"if true: hatchery does not create openstack image when a worker model is updated" json:"disableCreateImage"`

	// CreateImageTimeout max wait for create an openstack image (in seconds)
	CreateImageTimeout int `mapstructure:"createImageTimeout" toml:"createImageTimeout" default:"180" commented:"false" comment:"max wait for create an openstack image (in seconds)" json:"createImageTimeout"`

	// AllowedFlavors if not empty the hatchery will be able to start a model only if its flavor is listed in allowed flavors
	AllowedFlavors []string `mapstructure:"allowedFlavors" toml:"allowedFlavors" default:"" commented:"true" comment:"List of allowed flavors that can be used by the hatchery." json:"allowedFlavors"`

	// MaxCPUs if set the hatchery will stop starting new models if its flavors requires more CPUs than availables
	MaxCPUs int `mapstructure:"maxCpus" toml:"maxCpus" default:"" commented:"true" comment:"Maximum count of CPUs that can be used at a same time by the hatchery." json:"maxCpus"`

	// CountSmallerFlavorToKeep define the count of smaller flavors that the hatchery should be able to boot when booting a larger flavor.
	// This count will prevent big flavor to take all the CPUs available for the hatchery and will keep some available for smaller flavors.
	// Ex: if two flavors are available with 8 and 4 cpus and count to keep equals 2 the hatchery will need 8+4*2=16cpus available to start a 8cpus flavor.
	CountSmallerFlavorToKeep int `mapstructure:"countSmallerFlavorToKeep" toml:"countSmallerFlavorToKeep" default:"" commented:"true" comment:"Count of smaller flavors that the hatchery should be able to boot when booting a larger flavor." json:"countSmallerFlavorToKeep"`

	// DefaultFlavor, if set the hatchery will use a model with the default flavor in priority to start jobs without model requirement
	DefaultFlavor string `mapstructure:"defaultFlavor" toml:"defaultFlavor" default:"" commented:"true" comment:"If set the hatchery will use a model with the default flavor in priority to start jobs without model requirement" json:"defaultFlavor"`

	// DefaultSecurityGroups, if set the VMs spawned by the hatchery will have the given security groups
	DefaultSecurityGroups []string `mapstructure:"defaultSecurityGroups" toml:"defaultSecurityGroups" default:"" commented:"true" comment:"If set the hatchery will add given groups on spawned VMs" json:"defaultSecurityGroups"`

	Cache struct {
		ImagesExpirationDelay  int `mapstructure:"imagesExpirationDelay" toml:"imagesExpirationDelay" default:"30" commented:"true" comment:"Expiration delay for Openstack images list cache (in seconds)." json:"imagesExpirationDelay"`
		ServersExpirationDelay int `mapstructure:"serversExpirationDelay" toml:"serversExpirationDelay" default:"2" commented:"true" comment:"Expiration delay for Openstack servers list cache (in seconds)." json:"serversExpirationDelay"`
	} `mapstructure:"cache" toml:"cache" json:"cache"`

	// OverrideImagesUsername define the username to use for the specified image.
	OverrideImagesUsername []ImageUsernameOverride `mapstructure:"overrideImagesUsername" toml:"overrideImagesUsername" default:"" commented:"true" comment:"Override the user used for the images" json:"overrideImagesUsername"`

	RequiredBinariesRequirement []string `mapstructure:"requiredBinariesRequirement" toml:"requiredBinariesRequirement" default:"" commented:"true" comment:"If a job don't have any model requirement, check if there is at least required binaries" json:"requiredBinariesRequirement"`
}

// HatcheryOpenstack spawns instances of worker model with type 'ISO'
// by startup up virtual machines on /cloud
type HatcheryOpenstack struct {
	hatcheryCommon.Common
	Config          HatcheryConfiguration
	flavors         []flavors.Flavor
	openstackClient *gophercloud.ServiceClient
	cache           *cache
	networkID       string                    // computed from networkString
	imagesUsername  map[*regexp.Regexp]string // computed from initImagesUsername
}

type ipInfos struct {
	workerName     string
	dateLastBooked time.Time
}

type ImageUsernameOverride struct {
	Image    string `mapstructure:"image" toml:"image" default:"" commented:"true" comment:"The image name regular expression." json:"image"`
	Username string `mapstructure:"username" toml:"username" default:"" commented:"true" comment:"The username to use." json:"username"`
}
