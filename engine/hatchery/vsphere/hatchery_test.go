package vsphere

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ovh/cds/engine/hatchery"
	"github.com/ovh/cds/engine/service"
	"github.com/ovh/cds/sdk"
	"github.com/ovh/cds/sdk/cdsclient/mock_cdsclient"
	sdkhatchery "github.com/ovh/cds/sdk/hatchery"
	"github.com/rockbears/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
	"go.uber.org/mock/gomock"
)

func TestHatcheryVSphere_CanSpawnv1(t *testing.T) {
	log.Factory = log.NewTestingWrapper(t)

	c := NewVSphereClientTest(t)
	h := HatcheryVSphere{
		vSphereClient: c,
	}

	var ctx = context.Background()
	var invalidModel = sdk.Model{}
	var validModel = sdk.Model{
		Name: "model",
		Type: sdk.VSphere,
		ModelVirtualMachine: sdk.ModelVirtualMachine{
			Cmd: "cmd",
		}}

	can := h.CanSpawn(ctx, sdk.WorkerStarterWorkerModel{ModelV1: &invalidModel}, "1", []sdk.Requirement{{Type: sdk.ModelRequirement}})
	assert.False(t, can, "without a model VSphere, it should return False")

	can = h.CanSpawn(ctx, sdk.WorkerStarterWorkerModel{ModelV1: &validModel}, "1", []sdk.Requirement{{Type: sdk.ServiceRequirement}})
	assert.False(t, can, "without a service requirement, it should return False")

	can = h.CanSpawn(ctx, sdk.WorkerStarterWorkerModel{ModelV1: &validModel}, "1", []sdk.Requirement{{Type: sdk.MemoryRequirement}})
	assert.False(t, can, "without a memory requirement, it should return False")

	can = h.CanSpawn(ctx, sdk.WorkerStarterWorkerModel{ModelV1: &validModel}, "1", []sdk.Requirement{{Type: sdk.HostnameRequirement}})
	assert.False(t, can, "without a hostname requirement, it should return False")

	c.EXPECT().ListVirtualMachines(gomock.Any()).DoAndReturn(func(ctx context.Context) ([]mo.VirtualMachine, error) {
		return []mo.VirtualMachine{
			{
				ManagedEntity: mo.ManagedEntity{
					Name: "worker1",
				},
				Summary: types.VirtualMachineSummary{
					Config: types.VirtualMachineConfigSummary{
						Template: false,
					},
				},
				Config: &types.VirtualMachineConfigInfo{
					Annotation: `{"job_id": "1"}`,
				},
			},
		}, nil
	})

	can = h.CanSpawn(ctx, sdk.WorkerStarterWorkerModel{ModelV1: &validModel}, "1", []sdk.Requirement{})
	assert.False(t, can, "it should return False, because there is a worker for the same job")

	c.EXPECT().ListVirtualMachines(gomock.Any()).DoAndReturn(func(ctx context.Context) ([]mo.VirtualMachine, error) {
		return []mo.VirtualMachine{
			{
				ManagedEntity: mo.ManagedEntity{
					Name: "worker1",
				},
				Summary: types.VirtualMachineSummary{
					Config: types.VirtualMachineConfigSummary{
						Template: false,
					},
				},
				Config: &types.VirtualMachineConfigInfo{
					Annotation: `{"job_id": 2}`,
				},
			},
		}, nil
	})

	can = h.CanSpawn(ctx, sdk.WorkerStarterWorkerModel{ModelV1: &validModel}, "1", []sdk.Requirement{})
	assert.True(t, can, "it should return True")

	c.EXPECT().ListVirtualMachines(gomock.Any()).DoAndReturn(func(ctx context.Context) ([]mo.VirtualMachine, error) {
		return []mo.VirtualMachine{}, nil
	})
	can = h.CanSpawn(ctx, sdk.WorkerStarterWorkerModel{ModelV1: &validModel}, "0", []sdk.Requirement{})
	assert.True(t, can, "it should return True")

	c.EXPECT().ListVirtualMachines(gomock.Any()).DoAndReturn(func(ctx context.Context) ([]mo.VirtualMachine, error) {
		return []mo.VirtualMachine{
			{
				ManagedEntity: mo.ManagedEntity{
					Name: validModel.Name + "-tmp",
				},
				Config: &types.VirtualMachineConfigInfo{
					Annotation: fmt.Sprintf(`{"worker_model_path": "%s"}`, validModel.Name),
				},
			},
		}, nil
	}).AnyTimes()
	can = h.CanSpawn(ctx, sdk.WorkerStarterWorkerModel{ModelV1: &validModel}, "0", []sdk.Requirement{})
	assert.False(t, can, "with a 'tmp' vm, it should return False")

	c.EXPECT().ListVirtualMachines(gomock.Any()).DoAndReturn(func(ctx context.Context) ([]mo.VirtualMachine, error) {
		return []mo.VirtualMachine{
			{
				ManagedEntity: mo.ManagedEntity{
					Name: "register-" + validModel.Name + "-blabla",
				},
				Config: &types.VirtualMachineConfigInfo{
					Annotation: fmt.Sprintf(`{"worker_model_path": "%s"}`, validModel.Name),
				},
			},
		}, nil
	}).AnyTimes()
	can = h.CanSpawn(ctx, sdk.WorkerStarterWorkerModel{ModelV1: &validModel}, "0", []sdk.Requirement{})
	assert.False(t, can, "with a 'register' vm, it should return False")

	h.cachePendingJobID.list = append(h.cachePendingJobID.list, "666")
	c.EXPECT().ListVirtualMachines(gomock.Any()).DoAndReturn(func(ctx context.Context) ([]mo.VirtualMachine, error) {
		return []mo.VirtualMachine{}, nil
	}).AnyTimes()

	can = h.CanSpawn(ctx, sdk.WorkerStarterWorkerModel{ModelV1: &validModel}, "666", []sdk.Requirement{})
	assert.False(t, can, "it should return False because the jobID is still in the local cache")
}

func TestHatcheryVSphere_NeedRegistration(t *testing.T) {
	log.Factory = log.NewTestingWrapper(t)

	c := NewVSphereClientTest(t)
	h := HatcheryVSphere{
		vSphereClient: c,
		Config: HatcheryConfiguration{
			GuestCredentials: []GuestCredential{
				{
					ModelPath: "model",
					Username:  "user",
					Password:  "password",
				},
			},
		},
	}

	var ctx = context.Background()
	var now = time.Now()
	var validModel = sdk.Model{
		Name: "model",
		Type: sdk.VSphere,
		ModelVirtualMachine: sdk.ModelVirtualMachine{
			Cmd: "cmd",
		},
		UserLastModified: now,
	}

	// Without any VM returned by vSphere, it should return True
	c.EXPECT().ListVirtualMachines(gomock.Any()).DoAndReturn(func(ctx context.Context) ([]mo.VirtualMachine, error) {
		return []mo.VirtualMachine{}, nil
	})
	assert.True(t, h.NeedRegistration(ctx, &validModel), "without any VM returned by vSphere, it should return True")

	// vSphere returns a VM Template maching to te model, it should return False
	c.EXPECT().ListVirtualMachines(gomock.Any()).DoAndReturn(func(ctx context.Context) ([]mo.VirtualMachine, error) {
		return []mo.VirtualMachine{
			{
				ManagedEntity: mo.ManagedEntity{
					Name: "model",
				},
				Summary: types.VirtualMachineSummary{
					Config: types.VirtualMachineConfigSummary{
						Template: true,
					},
				},
				Config: &types.VirtualMachineConfigInfo{
					Annotation: fmt.Sprintf(`{"worker_model_last_modified": "%d", "model": true}`, now.Unix()),
				},
			},
		}, nil
	}).AnyTimes()
	assert.False(t, h.NeedRegistration(ctx, &validModel), "vSphere returns a VM Template maching to te model, it should return False")

}

func TestHatcheryVSphere_killDisabledWorkers(t *testing.T) {
	log.Factory = log.NewTestingWrapper(t)

	c := NewVSphereClientTest(t)
	sdkhatchery.InitMetrics(context.Background())

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	cdsclient := mock_cdsclient.NewMockInterface(ctrl)

	h := HatcheryVSphere{
		vSphereClient: c,
		Common: hatchery.Common{
			Common: service.Common{
				Client: cdsclient,
			},
		},
	}

	cdsclient.EXPECT().WorkerList(gomock.Any()).DoAndReturn(
		func(ctx context.Context) ([]sdk.Worker, error) {
			var un int64 = 1
			return []sdk.Worker{
				{
					Name:    "worker1",
					ModelID: &un,
					Status:  sdk.StatusDisabled,
				},
			}, nil
		},
	)

	c.EXPECT().ListVirtualMachines(gomock.Any()).DoAndReturn(
		func(ctx context.Context) ([]mo.VirtualMachine, error) {
			return []mo.VirtualMachine{
				{
					ManagedEntity: mo.ManagedEntity{
						Name: "worker1",
					},
					Summary: types.VirtualMachineSummary{
						Config: types.VirtualMachineConfigSummary{
							Template: false,
						},
					},
					Config: &types.VirtualMachineConfigInfo{
						Annotation: `{"model": false}`,
					},
				},
			}, nil
		},
	).AnyTimes()

	h.killDisabledWorkers(context.Background())

	assert.Equal(t, 1, len(h.cacheToDelete.list))
}

func TestHatcheryVSphere_killAwolServers(t *testing.T) {
	log.Factory = log.NewTestingWrapper(t)

	c := NewVSphereClientTest(t)
	sdkhatchery.InitMetrics(context.Background())

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	cdsclient := mock_cdsclient.NewMockInterface(ctrl)

	h := HatcheryVSphere{
		vSphereClient: c,
		Common: hatchery.Common{
			Common: service.Common{
				Client: cdsclient,
			},
		},
	}
	h.Config.WorkerTTL = 5
	h.Config.WorkerRegistrationTTL = 5

	cdsclient.EXPECT().WorkerList(gomock.Any()).DoAndReturn(
		func(ctx context.Context) ([]sdk.Worker, error) {
			var un int64 = 1
			return []sdk.Worker{
				{
					Name:    "worker1",
					ModelID: &un,
					Status:  sdk.StatusDisabled,
				},
			}, nil
		},
	)

	c.EXPECT().GetVirtualMachinePowerState(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, vm *object.VirtualMachine) (types.VirtualMachinePowerState, error) {
			switch vm.Name() {
			case "worker0":
				return types.VirtualMachinePowerStatePoweredOn, nil
			default:
				return types.VirtualMachinePowerStatePoweredOff, nil
			}
		},
	).AnyTimes()

	c.EXPECT().LoadVirtualMachineEvents(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, vm *object.VirtualMachine, eventTypes ...string) ([]types.BaseEvent, error) {
			t.Logf("LoadVirtualMachineEvents for %s", vm.Name())
			switch vm.Name() {
			case "worker0":
				return []types.BaseEvent{
					&types.VmStartingEvent{
						VmEvent: types.VmEvent{
							Event: types.Event{
								CreatedTime: time.Now(),
							},
						},
					},
				}, nil
			default:
				return []types.BaseEvent{
					&types.VmStartingEvent{
						VmEvent: types.VmEvent{
							Event: types.Event{
								CreatedTime: time.Now().Add(-10 * time.Minute),
							},
						},
					},
				}, nil
			}
		},
	).Times(5)

	c.EXPECT().ListVirtualMachines(gomock.Any()).DoAndReturn(
		func(ctx context.Context) ([]mo.VirtualMachine, error) {
			return []mo.VirtualMachine{
				{
					ManagedEntity: mo.ManagedEntity{
						Name: "worker0",
					},
					Summary: types.VirtualMachineSummary{
						Config: types.VirtualMachineConfigSummary{
							Template: false,
						},
						Runtime: types.VirtualMachineRuntimeInfo{
							PowerState: types.VirtualMachinePowerStatePoweredOn,
						},
					},
					Config: &types.VirtualMachineConfigInfo{
						Annotation: `{"model": false, "to_delete": false, "worker_model_path": "someting"}`,
					},
				},
				{
					ManagedEntity: mo.ManagedEntity{
						Name: "worker1",
					},
					Summary: types.VirtualMachineSummary{
						Config: types.VirtualMachineConfigSummary{
							Template: false,
						},
					},
					Config: &types.VirtualMachineConfigInfo{
						Annotation: `{"model": false, "to_delete": true}`,
					},
				},
				{ // Provisionned worker not used should be kept
					ManagedEntity: mo.ManagedEntity{
						Name: "provision-worker2",
					},
					Summary: types.VirtualMachineSummary{
						Config: types.VirtualMachineConfigSummary{
							Template: false,
						},
					},
					Config: &types.VirtualMachineConfigInfo{
						Annotation: `{"worker_name":"provision-worker2", "provisioning": true}`,
					},
				},
				{ // Provisionned worker already used should be deleted
					ManagedEntity: mo.ManagedEntity{
						Name: "worker3",
					},
					Summary: types.VirtualMachineSummary{
						Config: types.VirtualMachineConfigSummary{
							Template: false,
						},
					},
					Config: &types.VirtualMachineConfigInfo{
						Annotation: `{"worker_name":"provision-worker3", "provisioning": true}`,
					},
				},
			}, nil
		},
	).Times(1)

	var vm0 = object.VirtualMachine{
		Common: object.Common{
			InventoryPath: "worker0",
		},
	}
	var vm1 = object.VirtualMachine{Common: object.Common{InventoryPath: "worker1"}}
	var vm3 = object.VirtualMachine{Common: object.Common{InventoryPath: "worker3"}}

	c.EXPECT().LoadVirtualMachine(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, vmname string) (*object.VirtualMachine, error) {
			t.Logf("calling LoadVirtualMachine: %s", vmname)
			switch vmname {
			case "worker0":
				return &vm0, nil
			case "worker1":
				return &vm1, nil
			case "worker3":
				return &vm3, nil
			}
			return nil, fmt.Errorf("not expected: %s", vmname)
		},
	).Times(5)

	c.EXPECT().ShutdownVirtualMachine(gomock.Any(), &vm1).DoAndReturn(
		func(ctx context.Context, vm *object.VirtualMachine) error { return nil },
	)
	c.EXPECT().DestroyVirtualMachine(gomock.Any(), &vm1).DoAndReturn(
		func(ctx context.Context, vm *object.VirtualMachine) error { return nil },
	)
	c.EXPECT().ShutdownVirtualMachine(gomock.Any(), &vm3).DoAndReturn(
		func(ctx context.Context, vm *object.VirtualMachine) error { return nil },
	)
	c.EXPECT().DestroyVirtualMachine(gomock.Any(), &vm3).DoAndReturn(
		func(ctx context.Context, vm *object.VirtualMachine) error { return nil },
	)

	h.killAwolServers(context.Background())
}

func TestHatcheryVSphere_Status(t *testing.T) {
	log.Factory = log.NewTestingWrapper(t)

	c := NewVSphereClientTest(t)
	h := HatcheryVSphere{
		vSphereClient: c,
		Common: hatchery.Common{
			Common: service.Common{
				GoRoutines: sdk.NewGoRoutines(context.Background()),
			},
		},
	}

	c.EXPECT().ListVirtualMachines(gomock.Any()).Times(2).DoAndReturn(
		func(ctx context.Context) ([]mo.VirtualMachine, error) {
			return []mo.VirtualMachine{
				{
					ManagedEntity: mo.ManagedEntity{
						Name: "worker0",
					},
					Summary: types.VirtualMachineSummary{
						Config: types.VirtualMachineConfigSummary{
							Template: false,
						},
						Runtime: types.VirtualMachineRuntimeInfo{
							PowerState: types.VirtualMachinePowerStatePoweredOn,
						},
					},
					Config: &types.VirtualMachineConfigInfo{
						Annotation: `{"model": false, "to_delete": false, "worker_model_path": "someting"}`,
					},
				},
				{
					ManagedEntity: mo.ManagedEntity{
						Name: "worker1",
					},
					Summary: types.VirtualMachineSummary{
						Config: types.VirtualMachineConfigSummary{
							Template: false,
						},
					},
					Config: &types.VirtualMachineConfigInfo{
						Annotation: `{"model": false, "to_delete": true}`,
					},
				},
			}, nil
		},
	)

	s := h.Status(context.Background())
	t.Logf("status: %+v", s)
	assert.NotNil(t, s)
}

func TestHatcheryVSphere_provisioning_v1_do_nothing(t *testing.T) {
	log.Factory = log.NewTestingWrapper(t)

	var validModel = sdk.Model{
		Name: "model",
		Type: sdk.VSphere,
		ModelVirtualMachine: sdk.ModelVirtualMachine{
			Cmd:     "./worker",
			Image:   "model",
			PostCmd: "shutdown -h now",
		},
		Group: &sdk.Group{
			Name: sdk.SharedInfraGroupName,
		},
	}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	cdsclient := mock_cdsclient.NewMockInterface(ctrl)

	c := NewVSphereClientTest(t)
	h := HatcheryVSphere{
		vSphereClient: c,
		Common: hatchery.Common{
			Common: service.Common{
				GoRoutines: sdk.NewGoRoutines(context.Background()),
				Client:     cdsclient,
			},
		},
		Config: HatcheryConfiguration{
			WorkerProvisioning: []WorkerProvisioningConfig{
				{
					ModelPath: sdk.SharedInfraGroupName + "/" + validModel.Name,
					Number:    1,
				},
			},
		},
	}

	var now = time.Now()

	c.EXPECT().ListVirtualMachines(gomock.Any()).DoAndReturn(
		func(ctx context.Context) ([]mo.VirtualMachine, error) {
			return []mo.VirtualMachine{
				{
					ManagedEntity: mo.ManagedEntity{
						Name: validModel.Name,
					},
					Summary: types.VirtualMachineSummary{
						Config: types.VirtualMachineConfigSummary{
							Template: true,
						},
					},
					Config: &types.VirtualMachineConfigInfo{
						Annotation: fmt.Sprintf(`{"worker_model_last_modified": "%d", "model": true}`, now.Unix()),
					},
				}, {
					ManagedEntity: mo.ManagedEntity{
						Name: "provision-v1-worker",
					},
					Summary: types.VirtualMachineSummary{
						Config: types.VirtualMachineConfigSummary{
							Template: false,
						},
					},
					Config: &types.VirtualMachineConfigInfo{
						Annotation: fmt.Sprintf(`{"worker_model_last_modified": "%d", "model": false, "worker_model_path": "%s", "provisioning": true}`, now.Unix(), sdk.SharedInfraGroupName+"/"+validModel.Name),
					},
					Runtime: types.VirtualMachineRuntimeInfo{
						PowerState: types.VirtualMachinePowerStatePoweredOff,
					},
				},
			}, nil
		},
	)

	cdsclient.EXPECT().WorkerModelGet(sdk.SharedInfraGroupName, validModel.Name).DoAndReturn(
		func(groupName, name string) (sdk.Model, error) {
			return validModel, nil
		},
	)

	h.provisioningV1(context.Background())
}

func TestHatcheryVSphere_provisioning_v2_do_nothing(t *testing.T) {
	log.Factory = log.NewTestingWrapper(t)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	cdsclient := mock_cdsclient.NewMockInterface(ctrl)

	c := NewVSphereClientTest(t)
	h := HatcheryVSphere{
		vSphereClient: c,
		Common: hatchery.Common{
			Common: service.Common{
				GoRoutines: sdk.NewGoRoutines(context.Background()),
				Client:     cdsclient,
			},
		},
		Config: HatcheryConfiguration{
			WorkerProvisioning: []WorkerProvisioningConfig{
				{
					ModelVMWare: "the-model",
					Number:      1,
				},
			},
		},
	}

	c.EXPECT().ListVirtualMachines(gomock.Any()).DoAndReturn(
		func(ctx context.Context) ([]mo.VirtualMachine, error) {
			return []mo.VirtualMachine{
				{
					ManagedEntity: mo.ManagedEntity{
						Name: "provision-v2-worker",
					},
					Summary: types.VirtualMachineSummary{
						Config: types.VirtualMachineConfigSummary{
							Template: false,
						},
					},
					Config: &types.VirtualMachineConfigInfo{
						Annotation: `{"model": false, "vmware_model_path": "the-model", "provisioning": true}`,
					},
					Runtime: types.VirtualMachineRuntimeInfo{
						PowerState: types.VirtualMachinePowerStatePoweredOff,
					},
				},
			}, nil
		},
	)

	h.provisioningV2(context.Background())
}

func TestHatcheryVSphere_provisioning_v1_start_one(t *testing.T) {
	log.Factory = log.NewTestingWrapper(t)

	var validModel = sdk.Model{
		Name: "model",
		Type: sdk.VSphere,
		ModelVirtualMachine: sdk.ModelVirtualMachine{
			Cmd:     "./worker",
			Image:   "model",
			PostCmd: "shutdown -h now",
		},
		Group: &sdk.Group{
			Name: sdk.SharedInfraGroupName,
		},
	}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	cdsclient := mock_cdsclient.NewMockInterface(ctrl)

	c := NewVSphereClientTest(t)
	h := HatcheryVSphere{
		vSphereClient: c,
		Common: hatchery.Common{
			Common: service.Common{
				GoRoutines: sdk.NewGoRoutines(context.Background()),
				Client:     cdsclient,
			},
		},
		Config: HatcheryConfiguration{
			WorkerProvisioning: []WorkerProvisioningConfig{
				{
					ModelPath: sdk.SharedInfraGroupName + "/" + validModel.Name,
					Number:    1,
				},
			},
		},
	}

	h.Config.VSphereNetworkString = "vbox-net"
	h.Config.VSphereCardName = "ethernet-card"
	h.Config.VSphereDatastoreString = "datastore"
	h.availableIPAddresses = []string{"192.168.0.1", "192.168.0.2", "192.168.0.3"}
	h.Config.Gateway = "192.168.0.254"
	h.Config.DNS = "192.168.0.253"

	var now = time.Now()

	c.EXPECT().ListVirtualMachines(gomock.Any()).DoAndReturn(
		func(ctx context.Context) ([]mo.VirtualMachine, error) {
			return []mo.VirtualMachine{
				{
					ManagedEntity: mo.ManagedEntity{
						Name: validModel.Name,
					},
					Summary: types.VirtualMachineSummary{
						Config: types.VirtualMachineConfigSummary{
							Template: true,
						},
					},
					Config: &types.VirtualMachineConfigInfo{
						Annotation: fmt.Sprintf(`{"worker_model_last_modified": "%d", "model": true}`, now.Unix()),
					},
				},
			}, nil
		},
	).AnyTimes()

	cdsclient.EXPECT().WorkerModelGet(sdk.SharedInfraGroupName, validModel.Name).DoAndReturn(
		func(groupName, name string) (sdk.Model, error) {
			return validModel, nil
		},
	)

	var vmTemplate = object.VirtualMachine{
		Common: object.Common{},
	}

	c.EXPECT().LoadVirtualMachine(gomock.Any(), validModel.Name).DoAndReturn(
		func(ctx context.Context, name string) (*object.VirtualMachine, error) {
			return &vmTemplate, nil
		},
	)

	c.EXPECT().LoadVirtualMachineDevices(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, vm *object.VirtualMachine) (object.VirtualDeviceList, error) {
			card := types.VirtualEthernetCard{}
			return object.VirtualDeviceList{
				&card,
			}, nil
		},
	)

	c.EXPECT().LoadNetwork(gomock.Any(), "vbox-net").DoAndReturn(
		func(ctx context.Context, s string) (object.NetworkReference, error) {
			return &object.Network{}, nil
		},
	)

	c.EXPECT().SetupEthernetCard(gomock.Any(), gomock.Any(), "ethernet-card", gomock.Any()).DoAndReturn(
		func(ctx context.Context, card *types.VirtualEthernetCard, ethernetCardName string, network object.NetworkReference) error {
			return nil
		},
	)

	c.EXPECT().LoadResourcePool(gomock.Any()).DoAndReturn(
		func(ctx context.Context) (*object.ResourcePool, error) {
			return &object.ResourcePool{}, nil
		},
	)

	c.EXPECT().LoadDatastore(gomock.Any(), "datastore").DoAndReturn(
		func(ctx context.Context, name string) (*object.Datastore, error) {
			return &object.Datastore{}, nil
		},
	)

	var folder object.Folder

	c.EXPECT().LoadFolder(gomock.Any()).DoAndReturn(
		func(ctx context.Context) (*object.Folder, error) {
			return &folder, nil
		},
	)

	var workerRef types.ManagedObjectReference

	c.EXPECT().CloneVirtualMachine(gomock.Any(), &vmTemplate, &folder, gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, vm *object.VirtualMachine, folder *object.Folder, name string, config *types.VirtualMachineCloneSpec) (*types.ManagedObjectReference, error) {
			assert.True(t, strings.HasPrefix(name, "provision-"))
			return &workerRef, nil
		},
	)

	var workerVM object.VirtualMachine

	c.EXPECT().NewVirtualMachine(gomock.Any(), gomock.Any(), &workerRef, gomock.Any()).DoAndReturn(
		func(ctx context.Context, cloneSpec *types.VirtualMachineCloneSpec, ref *types.ManagedObjectReference, vmName string) (*object.VirtualMachine, error) {
			assert.False(t, cloneSpec.Template)
			assert.True(t, cloneSpec.PowerOn)
			var givenAnnotation annotation
			json.Unmarshal([]byte(cloneSpec.Config.Annotation), &givenAnnotation)
			assert.Equal(t, "shared.infra/model", givenAnnotation.WorkerModelPath)
			assert.False(t, givenAnnotation.Model)
			assert.Equal(t, "192.168.0.1", (cloneSpec.Customization.NicSettingMap[0].Adapter.Ip.(*types.CustomizationFixedIp).IpAddress))
			return &workerVM, nil
		},
	)

	c.EXPECT().WaitForVirtualMachineIP(gomock.Any(), &workerVM, gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, vm *object.VirtualMachine, _ *string, _ string) error {
			return nil
		},
	)

	c.EXPECT().ShutdownVirtualMachine(gomock.Any(), &workerVM).DoAndReturn(
		func(ctx context.Context, vm *object.VirtualMachine) error {
			return nil
		},
	)

	h.provisioningV1(context.Background())
}

func TestHatcheryVSphere_GetDetaultModelV2Name(t *testing.T) {
	h := HatcheryVSphere{
		Config: HatcheryConfiguration{
			DefaultWorkerModelsV2: []DefaultWorkerModelsV2{
				{
					WorkerModelV2: "the-model-v2",
					Binaries:      []string{"docker"},
				},
			},
		},
	}

	requirements := []sdk.Requirement{
		{
			Name:  "binary",
			Value: "docker",
			Type:  sdk.BinaryRequirement,
		},
	}
	got := h.GetDetaultModelV2Name(context.TODO(), requirements)
	require.Equal(t, "the-model-v2", got)

	got = h.GetDetaultModelV2Name(context.TODO(), []sdk.Requirement{})
	require.Equal(t, "the-model-v2", got)

	got = h.GetDetaultModelV2Name(context.TODO(), []sdk.Requirement{{Name: "foo", Value: "bar", Type: sdk.BinaryRequirement}, {Name: "foo", Value: "docker", Type: sdk.BinaryRequirement}})
	require.Equal(t, "the-model-v2", got)

	got = h.GetDetaultModelV2Name(context.TODO(), []sdk.Requirement{{Name: "foo", Value: "bar", Type: sdk.BinaryRequirement}})
	require.Equal(t, "", got)
}
