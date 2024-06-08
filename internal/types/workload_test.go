package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_ApplyProfile(t *testing.T) {
	tests := []struct {
		name     string
		workload Workload
		profile  *Profile
		want     EffectiveWorkload
	}{
		{
			name: "Camera ON: workload ON + profile ON",
			workload: Workload{
				HostAccess: HostAccess{Camera: true},
			},
			profile: &Profile{
				HostAccess: HostAccess{Camera: true},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Camera: true},
				},
				Profile: &Profile{
					HostAccess: HostAccess{Camera: true},
				},
			},
		},
		{
			name: "Camera OFF: workload OFF + profile ON",
			workload: Workload{
				HostAccess: HostAccess{Camera: false},
			},
			profile: &Profile{
				HostAccess: HostAccess{Camera: true},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Camera: false},
				},
				Profile: &Profile{
					HostAccess: HostAccess{Camera: true},
				},
			},
		},
		{
			name: "Camera OFF: workload ON + profile OFF",
			workload: Workload{
				HostAccess: HostAccess{Camera: true},
			},
			profile: &Profile{
				HostAccess: HostAccess{Camera: false},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Camera: false},
				},
				Profile: &Profile{
					HostAccess: HostAccess{Camera: false},
				},
			},
		},
		{
			name: "Camera OFF: workload OFF + profile OFF",
			workload: Workload{
				HostAccess: HostAccess{Camera: false},
			},
			profile: &Profile{
				HostAccess: HostAccess{Camera: false},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Camera: false},
				},
				Profile: &Profile{
					HostAccess: HostAccess{Camera: false},
				},
			},
		},
		{
			name: "X11 ON: workload ON + profile ON",
			workload: Workload{
				HostAccess: HostAccess{X11: true},
			},
			profile: &Profile{
				HostAccess: HostAccess{X11: true},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{X11: true},
				},
				Profile: &Profile{
					HostAccess: HostAccess{X11: true},
				},
			},
		},
		{
			name: "X11 OFF: workload OFF + profile ON",
			workload: Workload{
				HostAccess: HostAccess{X11: false},
			},
			profile: &Profile{
				HostAccess: HostAccess{X11: true},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{X11: false},
				},
				Profile: &Profile{
					HostAccess: HostAccess{X11: true},
				},
			},
		},
		{
			name: "X11 OFF: workload ON + profile OFF",
			workload: Workload{
				HostAccess: HostAccess{X11: true},
			},
			profile: &Profile{
				HostAccess: HostAccess{X11: false},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{X11: false},
				},
				Profile: &Profile{
					HostAccess: HostAccess{X11: false},
				},
			},
		},
		{
			name: "X11 OFF: workload OFF + profile OFF",
			workload: Workload{
				HostAccess: HostAccess{X11: false},
			},
			profile: &Profile{
				HostAccess: HostAccess{X11: false},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{X11: false},
				},
				Profile: &Profile{
					HostAccess: HostAccess{X11: false},
				},
			},
		},
		{
			name: "Microphone ON: workload ON + profile ON",
			workload: Workload{
				HostAccess: HostAccess{Microphone: true},
			},
			profile: &Profile{
				HostAccess: HostAccess{Microphone: true},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Microphone: true},
				},
				Profile: &Profile{
					HostAccess: HostAccess{Microphone: true},
				},
			},
		},
		{
			name: "Microphone OFF: workload OFF + profile ON",
			workload: Workload{
				HostAccess: HostAccess{Microphone: false},
			},
			profile: &Profile{
				HostAccess: HostAccess{Microphone: true},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Microphone: false},
				},
				Profile: &Profile{
					HostAccess: HostAccess{Microphone: true},
				},
			},
		},
		{
			name: "Microphone OFF: workload ON + profile OFF",
			workload: Workload{
				HostAccess: HostAccess{Microphone: true},
			},
			profile: &Profile{
				HostAccess: HostAccess{Microphone: false},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Microphone: false},
				},
				Profile: &Profile{
					HostAccess: HostAccess{Microphone: false},
				},
			},
		},
		{
			name: "Microphone OFF: workload OFF + profile OFF",
			workload: Workload{
				HostAccess: HostAccess{Microphone: false},
			},
			profile: &Profile{
				HostAccess: HostAccess{Microphone: false},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Microphone: false},
				},
				Profile: &Profile{
					HostAccess: HostAccess{Microphone: false},
				},
			},
		},
		{
			name: "Speakers ON: workload ON + profile ON",
			workload: Workload{
				HostAccess: HostAccess{Speakers: true},
			},
			profile: &Profile{
				HostAccess: HostAccess{Speakers: true},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Speakers: true},
				},
				Profile: &Profile{
					HostAccess: HostAccess{Speakers: true},
				},
			},
		},
		{
			name: "Speakers OFF: workload OFF + profile ON",
			workload: Workload{
				HostAccess: HostAccess{Speakers: false},
			},
			profile: &Profile{
				HostAccess: HostAccess{Speakers: true},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Speakers: false},
				},
				Profile: &Profile{
					HostAccess: HostAccess{Speakers: true},
				},
			},
		},
		{
			name: "Speakers OFF: workload ON + profile OFF",
			workload: Workload{
				HostAccess: HostAccess{Speakers: true},
			},
			profile: &Profile{
				HostAccess: HostAccess{Speakers: false},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Speakers: false},
				},
				Profile: &Profile{
					HostAccess: HostAccess{Speakers: false},
				},
			},
		},
		{
			name: "Speakers OFF: workload OFF + profile OFF",
			workload: Workload{
				HostAccess: HostAccess{Speakers: false},
			},
			profile: &Profile{
				HostAccess: HostAccess{Speakers: false},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Speakers: false},
				},
				Profile: &Profile{
					HostAccess: HostAccess{Speakers: false},
				},
			},
		},
		{
			name: "Smartcard ON: workload ON + profile ON",
			workload: Workload{
				HostAccess: HostAccess{Smartcard: true},
			},
			profile: &Profile{
				HostAccess: HostAccess{Smartcard: true},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Smartcard: true},
				},
				Profile: &Profile{
					HostAccess: HostAccess{Smartcard: true},
				},
			},
		},
		{
			name: "Smartcard OFF: workload OFF + profile ON",
			workload: Workload{
				HostAccess: HostAccess{Smartcard: false},
			},
			profile: &Profile{
				HostAccess: HostAccess{Smartcard: true},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Smartcard: false},
				},
				Profile: &Profile{
					HostAccess: HostAccess{Smartcard: true},
				},
			},
		},
		{
			name: "Smartcard OFF: workload ON + profile OFF",
			workload: Workload{
				HostAccess: HostAccess{Smartcard: true},
			},
			profile: &Profile{
				HostAccess: HostAccess{Smartcard: false},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Smartcard: false},
				},
				Profile: &Profile{
					HostAccess: HostAccess{Smartcard: false},
				},
			},
		},
		{
			name: "Smartcard OFF: workload OFF + profile OFF",
			workload: Workload{
				HostAccess: HostAccess{Smartcard: false},
			},
			profile: &Profile{
				HostAccess: HostAccess{Smartcard: false},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Smartcard: false},
				},
				Profile: &Profile{
					HostAccess: HostAccess{Smartcard: false},
				},
			},
		},
		{
			name: "VarRunUser ON: workload ON + profile ON",
			workload: Workload{
				HostAccess: HostAccess{VarRunUser: true},
			},
			profile: &Profile{
				HostAccess: HostAccess{VarRunUser: true},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{VarRunUser: true},
				},
				Profile: &Profile{
					HostAccess: HostAccess{VarRunUser: true},
				},
			},
		},
		{
			name: "VarRunUser OFF: workload OFF + profile ON",
			workload: Workload{
				HostAccess: HostAccess{VarRunUser: false},
			},
			profile: &Profile{
				HostAccess: HostAccess{VarRunUser: true},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{VarRunUser: false},
				},
				Profile: &Profile{
					HostAccess: HostAccess{VarRunUser: true},
				},
			},
		},
		{
			name: "VarRunUser OFF: workload ON + profile OFF",
			workload: Workload{
				HostAccess: HostAccess{VarRunUser: true},
			},
			profile: &Profile{
				HostAccess: HostAccess{VarRunUser: false},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{VarRunUser: false},
				},
				Profile: &Profile{
					HostAccess: HostAccess{VarRunUser: false},
				},
			},
		},
		{
			name: "VarRunUser OFF: workload OFF + profile OFF",
			workload: Workload{
				HostAccess: HostAccess{VarRunUser: false},
			},
			profile: &Profile{
				HostAccess: HostAccess{VarRunUser: false},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{VarRunUser: false},
				},
				Profile: &Profile{
					HostAccess: HostAccess{VarRunUser: false},
				},
			},
		},
		{
			name: "USBDevices: drop named devices not in profile",
			workload: Workload{
				HostAccess: HostAccess{
					USBDevices: []string{"Foo and Bar"},
				},
			},
			profile: &Profile{
				HostAccess: HostAccess{
					USBDevices: []string{},
				},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{
						USBDevices: nil,
					},
				},
				Profile: &Profile{
					HostAccess: HostAccess{
						USBDevices: []string{},
					},
				},
			},
		},
		{
			name: "USBDevices: add allowed named devices",
			workload: Workload{
				HostAccess: HostAccess{
					USBDevices: []string{
						"Foo and Bar",
						"Foo",
						"Bar",
					},
				},
			},
			profile: &Profile{
				HostAccess: HostAccess{
					USBDevices: []string{
						"Foo",
						"FooBar",
					},
				},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{
						USBDevices: []string{
							"Foo",
						},
					},
				},
				Profile: &Profile{
					HostAccess: HostAccess{
						USBDevices: []string{
							"Foo",
							"FooBar",
						},
					},
				},
			},
		},
		{
			name: "MachineID ON: workload ON + profile ON",
			workload: Workload{
				HostAccess: HostAccess{MachineID: true},
			},
			profile: &Profile{
				HostAccess: HostAccess{MachineID: true},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{MachineID: true},
				},
				Profile: &Profile{
					HostAccess: HostAccess{MachineID: true},
				},
			},
		},
		{
			name: "MachineID OFF: workload OFF + profile ON",
			workload: Workload{
				HostAccess: HostAccess{MachineID: false},
			},
			profile: &Profile{
				HostAccess: HostAccess{MachineID: true},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{MachineID: false},
				},
				Profile: &Profile{
					HostAccess: HostAccess{MachineID: true},
				},
			},
		},
		{
			name: "MachineID OFF: workload ON + profile OFF",
			workload: Workload{
				HostAccess: HostAccess{MachineID: true},
			},
			profile: &Profile{
				HostAccess: HostAccess{MachineID: false},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{MachineID: false},
				},
				Profile: &Profile{
					HostAccess: HostAccess{MachineID: false},
				},
			},
		},
		{
			name: "MachineID OFF: workload OFF + profile OFF",
			workload: Workload{
				HostAccess: HostAccess{MachineID: false},
			},
			profile: &Profile{
				HostAccess: HostAccess{MachineID: false},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{MachineID: false},
				},
				Profile: &Profile{
					HostAccess: HostAccess{MachineID: false},
				},
			},
		},
		{
			name: "LocalTime ON: workload ON + profile ON",
			workload: Workload{
				HostAccess: HostAccess{LocalTime: true},
			},
			profile: &Profile{
				HostAccess: HostAccess{LocalTime: true},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{LocalTime: true},
				},
				Profile: &Profile{
					HostAccess: HostAccess{LocalTime: true},
				},
			},
		},
		{
			name: "LocalTime OFF: workload OFF + profile ON",
			workload: Workload{
				HostAccess: HostAccess{LocalTime: false},
			},
			profile: &Profile{
				HostAccess: HostAccess{LocalTime: true},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{LocalTime: false},
				},
				Profile: &Profile{
					HostAccess: HostAccess{LocalTime: true},
				},
			},
		},
		{
			name: "LocalTime OFF: workload ON + profile OFF",
			workload: Workload{
				HostAccess: HostAccess{LocalTime: true},
			},
			profile: &Profile{
				HostAccess: HostAccess{LocalTime: false},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{LocalTime: false},
				},
				Profile: &Profile{
					HostAccess: HostAccess{LocalTime: false},
				},
			},
		},
		{
			name: "LocalTime OFF: workload OFF + profile OFF",
			workload: Workload{
				HostAccess: HostAccess{LocalTime: false},
			},
			profile: &Profile{
				HostAccess: HostAccess{LocalTime: false},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{LocalTime: false},
				},
				Profile: &Profile{
					HostAccess: HostAccess{LocalTime: false},
				},
			},
		},
		{
			name: "GPUs All: workload all + profile All",
			workload: Workload{
				HostAccess: HostAccess{Gpus: "all"},
			},
			profile: &Profile{
				HostAccess: HostAccess{Gpus: "all"},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Gpus: "all"},
				},
				Profile: &Profile{
					HostAccess: HostAccess{Gpus: "all"},
				},
			},
		},
		{
			name: "GPUs empty: workload empty + profile All",
			workload: Workload{
				HostAccess: HostAccess{Gpus: ""},
			},
			profile: &Profile{
				HostAccess: HostAccess{Gpus: "all"},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Gpus: ""},
				},
				Profile: &Profile{
					HostAccess: HostAccess{Gpus: "all"},
				},
			},
		},
		{
			name: "GPUs empty: workload all + profile empty",
			workload: Workload{
				HostAccess: HostAccess{Gpus: "all"},
			},
			profile: &Profile{
				HostAccess: HostAccess{Gpus: ""},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Gpus: ""},
				},
				Profile: &Profile{
					HostAccess: HostAccess{Gpus: ""},
				},
			},
		},
		{
			name: "GPUs empty: workload empty + profile empty",
			workload: Workload{
				HostAccess: HostAccess{Gpus: ""},
			},
			profile: &Profile{
				HostAccess: HostAccess{Gpus: ""},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Gpus: ""},
				},
				Profile: &Profile{
					HostAccess: HostAccess{Gpus: ""},
				},
			},
		},
		{
			name: "Privileged ON: workload ON + profile ON",
			workload: Workload{
				HostAccess: HostAccess{Privileged: true},
			},
			profile: &Profile{
				HostAccess: HostAccess{Privileged: true},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Privileged: true},
				},
				Profile: &Profile{
					HostAccess: HostAccess{Privileged: true},
				},
			},
		},
		{
			name: "Privileged OFF: workload OFF + profile ON",
			workload: Workload{
				HostAccess: HostAccess{Privileged: false},
			},
			profile: &Profile{
				HostAccess: HostAccess{Privileged: true},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Privileged: false},
				},
				Profile: &Profile{
					HostAccess: HostAccess{Privileged: true},
				},
			},
		},
		{
			name: "Privileged OFF: workload ON + profile OFF",
			workload: Workload{
				HostAccess: HostAccess{Privileged: true},
			},
			profile: &Profile{
				HostAccess: HostAccess{Privileged: false},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Privileged: false},
				},
				Profile: &Profile{
					HostAccess: HostAccess{Privileged: false},
				},
			},
		},
		{
			name: "Privileged OFF: workload OFF + profile OFF",
			workload: Workload{
				HostAccess: HostAccess{Privileged: false},
			},
			profile: &Profile{
				HostAccess: HostAccess{Privileged: false},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Privileged: false},
				},
				Profile: &Profile{
					HostAccess: HostAccess{Privileged: false},
				},
			},
		},
		{
			name: "Network empty: workload empty + profile empty",
			workload: Workload{
				HostAccess: HostAccess{Network: ""},
			},
			profile: &Profile{
				HostAccess: HostAccess{Network: ""},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Network: ""},
				},
				Profile: &Profile{
					HostAccess: HostAccess{Network: ""},
				},
			},
		},
		{
			name: "Network none: workload empty + profile none",
			workload: Workload{
				HostAccess: HostAccess{Network: ""},
			},
			profile: &Profile{
				HostAccess: HostAccess{Network: "none"},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Network: "none"},
				},
				Profile: &Profile{
					HostAccess: HostAccess{Network: "none"},
				},
			},
		},
		{
			name: "Network none: workload none + profile empty",
			workload: Workload{
				HostAccess: HostAccess{Network: "none"},
			},
			profile: &Profile{
				HostAccess: HostAccess{Network: ""},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Network: "none"},
				},
				Profile: &Profile{
					HostAccess: HostAccess{Network: ""},
				},
			},
		},
		{
			name: "Network foo: workload foo + profile foo",
			workload: Workload{
				HostAccess: HostAccess{Network: "foo"},
			},
			profile: &Profile{
				HostAccess: HostAccess{Network: "foo"},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Network: "foo"},
				},
				Profile: &Profile{
					HostAccess: HostAccess{Network: "foo"},
				},
			},
		},
		{
			name: "Network foo: workload empty + profile foo",
			workload: Workload{
				HostAccess: HostAccess{Network: ""},
			},
			profile: &Profile{
				HostAccess: HostAccess{Network: "foo"},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Network: "foo"},
				},
				Profile: &Profile{
					HostAccess: HostAccess{Network: "foo"},
				},
			},
		},
		{
			name: "Network empty: workload foo + profile empty",
			workload: Workload{
				HostAccess: HostAccess{Network: "foo"},
			},
			profile: &Profile{
				HostAccess: HostAccess{Network: ""},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Network: ""},
				},
				Profile: &Profile{
					HostAccess: HostAccess{Network: ""},
				},
			},
		},
		{
			name: "Paths empty: workload /foo + profile empty",
			workload: Workload{
				HostAccess: HostAccess{Paths: []string{"/foo:/foo"}},
			},
			profile: &Profile{
				HostAccess: HostAccess{Paths: []string{}},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Paths: []string{}},
				},
				Profile: &Profile{
					HostAccess: HostAccess{Paths: []string{}},
				},
			},
		},
		{
			name: "Paths /foo: workload /foo + profile /foo",
			workload: Workload{
				HostAccess: HostAccess{Paths: []string{"/foo:/foo"}},
			},
			profile: &Profile{
				HostAccess: HostAccess{Paths: []string{"/foo"}},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Paths: []string{"/foo:/foo"}},
				},
				Profile: &Profile{
					HostAccess: HostAccess{Paths: []string{"/foo"}},
				},
			},
		},
		{
			name: "Paths empty: workload /foo + profile /foo1",
			workload: Workload{
				HostAccess: HostAccess{Paths: []string{"/foo:/foo"}},
			},
			profile: &Profile{
				HostAccess: HostAccess{Paths: []string{"/foo1"}},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Paths: []string{}},
				},
				Profile: &Profile{
					HostAccess: HostAccess{Paths: []string{"/foo1"}},
				},
			},
		},
		{
			name: "Paths /foo: workload /foo + profile /foo/",
			workload: Workload{
				HostAccess: HostAccess{Paths: []string{"/foo:/foo"}},
			},
			profile: &Profile{
				HostAccess: HostAccess{Paths: []string{"/foo/"}},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Paths: []string{"/foo:/foo"}},
				},
				Profile: &Profile{
					HostAccess: HostAccess{Paths: []string{"/foo/"}},
				},
			},
		},
		{
			name: "Paths /foo/: workload /foo/ + profile /foo",
			workload: Workload{
				HostAccess: HostAccess{Paths: []string{"/foo/:/foo/"}},
			},
			profile: &Profile{
				HostAccess: HostAccess{Paths: []string{"/foo"}},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Paths: []string{"/foo/:/foo/"}},
				},
				Profile: &Profile{
					HostAccess: HostAccess{Paths: []string{"/foo"}},
				},
			},
		},
		{
			name: "Paths ${HOME}/bar: workload ${HOME}/bar + profile /home",
			workload: Workload{
				HostAccess: HostAccess{Paths: []string{"${HOME}/bar:/home/bar"}},
			},
			profile: &Profile{
				HostAccess: HostAccess{Paths: []string{"/home"}},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Paths: []string{"${HOME}/bar:/home/bar"}},
				},
				Profile: &Profile{
					HostAccess: HostAccess{Paths: []string{"/home"}},
				},
			},
		},
		{
			name: "CapsAdd empty: workload FOO + profile empty",
			workload: Workload{
				HostAccess: HostAccess{CapsAdd: []string{"FOO"}},
			},
			profile: &Profile{
				HostAccess: HostAccess{CapsAdd: []string{}},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{CapsAdd: []string{}},
				},
				Profile: &Profile{
					HostAccess: HostAccess{CapsAdd: []string{}},
				},
			},
		},
		{
			name: "CapsAdd FOO: workload FOO + profile FOO",
			workload: Workload{
				HostAccess: HostAccess{CapsAdd: []string{"FOO"}},
			},
			profile: &Profile{
				HostAccess: HostAccess{CapsAdd: []string{"FOO"}},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{CapsAdd: []string{"FOO"}},
				},
				Profile: &Profile{
					HostAccess: HostAccess{CapsAdd: []string{"FOO"}},
				},
			},
		},
		{
			name: "CapsAdd empty: workload FOO + profile FOOB",
			workload: Workload{
				HostAccess: HostAccess{CapsAdd: []string{"FOO"}},
			},
			profile: &Profile{
				HostAccess: HostAccess{CapsAdd: []string{"FOOB"}},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{CapsAdd: []string{}},
				},
				Profile: &Profile{
					HostAccess: HostAccess{CapsAdd: []string{"FOOB"}},
				},
			},
		},
		{
			name: "CapsAdd foo: workload foo + profile foo",
			workload: Workload{
				HostAccess: HostAccess{CapsAdd: []string{"foo"}},
			},
			profile: &Profile{
				HostAccess: HostAccess{CapsAdd: []string{"foo"}},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{CapsAdd: []string{"foo"}},
				},
				Profile: &Profile{
					HostAccess: HostAccess{CapsAdd: []string{"foo"}},
				},
			},
		},
		{
			name: "CapsAdd bar: workload bar + profile foo and bar",
			workload: Workload{
				HostAccess: HostAccess{CapsAdd: []string{"bar"}},
			},
			profile: &Profile{
				HostAccess: HostAccess{CapsAdd: []string{"foo", "bar"}},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{CapsAdd: []string{"bar"}},
				},
				Profile: &Profile{
					HostAccess: HostAccess{CapsAdd: []string{"foo", "bar"}},
				},
			},
		},
		{
			name: "CapsAdd empty: workload bar + profile foo",
			workload: Workload{
				HostAccess: HostAccess{CapsAdd: []string{"bar"}},
			},
			profile: &Profile{
				HostAccess: HostAccess{CapsAdd: []string{"foo"}},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{CapsAdd: []string{}},
				},
				Profile: &Profile{
					HostAccess: HostAccess{CapsAdd: []string{"foo"}},
				},
			},
		},

		{
			name: "Devices empty: workload /foo + profile empty",
			workload: Workload{
				HostAccess: HostAccess{Devices: []string{"/foo"}},
			},
			profile: &Profile{
				HostAccess: HostAccess{Devices: []string{}},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Devices: []string{}},
				},
				Profile: &Profile{
					HostAccess: HostAccess{Devices: []string{}},
				},
			},
		},
		{
			name: "Devices /foo: workload /foo + profile /foo",
			workload: Workload{
				HostAccess: HostAccess{Devices: []string{"/foo"}},
			},
			profile: &Profile{
				HostAccess: HostAccess{Devices: []string{"/foo"}},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Devices: []string{"/foo"}},
				},
				Profile: &Profile{
					HostAccess: HostAccess{Devices: []string{"/foo"}},
				},
			},
		},
		{
			name: "Devices /foo/: workload /foo/ + profile /foo",
			workload: Workload{
				HostAccess: HostAccess{Devices: []string{"/foo/"}},
			},
			profile: &Profile{
				HostAccess: HostAccess{Devices: []string{"/foo"}},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Devices: []string{"/foo/"}},
				},
				Profile: &Profile{
					HostAccess: HostAccess{Devices: []string{"/foo"}},
				},
			},
		},
		{
			name: "Devices empty: workload /foo + profile /foob",
			workload: Workload{
				HostAccess: HostAccess{Devices: []string{"/foo"}},
			},
			profile: &Profile{
				HostAccess: HostAccess{Devices: []string{"/foob"}},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Devices: []string{}},
				},
				Profile: &Profile{
					HostAccess: HostAccess{Devices: []string{"/foob"}},
				},
			},
		},
		{
			name: "Devices /foo: workload /foo + profile /foo",
			workload: Workload{
				HostAccess: HostAccess{Devices: []string{"/foo"}},
			},
			profile: &Profile{
				HostAccess: HostAccess{Devices: []string{"/foo"}},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Devices: []string{"/foo"}},
				},
				Profile: &Profile{
					HostAccess: HostAccess{Devices: []string{"/foo"}},
				},
			},
		},
		{
			name: "Devices /bar: workload /bar + profile /foo and /bar",
			workload: Workload{
				HostAccess: HostAccess{Devices: []string{"/bar"}},
			},
			profile: &Profile{
				HostAccess: HostAccess{Devices: []string{"/foo", "/bar"}},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Devices: []string{"/bar"}},
				},
				Profile: &Profile{
					HostAccess: HostAccess{Devices: []string{"/foo", "/bar"}},
				},
			},
		},
		{
			name: "Devices empty: workload /bar + profile /foo",
			workload: Workload{
				HostAccess: HostAccess{Devices: []string{"/bar"}},
			},
			profile: &Profile{
				HostAccess: HostAccess{Devices: []string{"/foo"}},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Devices: []string{}},
				},
				Profile: &Profile{
					HostAccess: HostAccess{Devices: []string{"/foo"}},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert := assert.New(t)

			got := tc.workload.ApplyProfile(tc.profile)

			assert.Equal(tc.want, got)
		})
	}
}
