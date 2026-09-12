package types

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
				Camera: true,
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Camera: true},
				},
				Profile: &Profile{
					Camera: true,
				},
			},
		},
		{
			name: "Camera OFF: workload OFF + profile ON",
			workload: Workload{
				HostAccess: HostAccess{Camera: false},
			},
			profile: &Profile{
				Camera: true,
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Camera: false},
				},
				Profile: &Profile{
					Camera: true,
				},
			},
		},
		{
			name: "Camera OFF: workload ON + profile OFF",
			workload: Workload{
				HostAccess: HostAccess{Camera: true},
			},
			profile: &Profile{
				Camera: false,
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Camera: false},
				},
				Profile: &Profile{
					Camera: false,
				},
			},
		},
		{
			name: "Camera OFF: workload OFF + profile OFF",
			workload: Workload{
				HostAccess: HostAccess{Camera: false},
			},
			profile: &Profile{
				Camera: false,
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Camera: false},
				},
				Profile: &Profile{
					Camera: false,
				},
			},
		},
		{
			name: "Dbus ON: workload ON + profile ON",
			workload: Workload{
				HostAccess: HostAccess{Dbus: true},
			},
			profile: &Profile{
				Dbus: true,
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Dbus: true},
				},
				Profile: &Profile{
					Dbus: true,
				},
			},
		},
		{
			name: "Dbus OFF: workload OFF + profile ON",
			workload: Workload{
				HostAccess: HostAccess{Dbus: false},
			},
			profile: &Profile{
				Dbus: true,
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Dbus: false},
				},
				Profile: &Profile{
					Dbus: true,
				},
			},
		},
		{
			name: "Dbus OFF: workload ON + profile OFF",
			workload: Workload{
				HostAccess: HostAccess{Dbus: true},
			},
			profile: &Profile{
				Dbus: false,
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Dbus: false},
				},
				Profile: &Profile{
					Dbus: false,
				},
			},
		},
		{
			name: "Dbus OFF: workload OFF + profile OFF",
			workload: Workload{
				HostAccess: HostAccess{Dbus: false},
			},
			profile: &Profile{
				Dbus: false,
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Dbus: false},
				},
				Profile: &Profile{
					Dbus: false,
				},
			},
		},
		{
			name: "Microphone ON: workload ON + profile ON",
			workload: Workload{
				HostAccess: HostAccess{Microphone: true},
			},
			profile: &Profile{
				Microphone: true,
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Microphone: true},
				},
				Profile: &Profile{
					Microphone: true,
				},
			},
		},
		{
			name: "Microphone OFF: workload OFF + profile ON",
			workload: Workload{
				HostAccess: HostAccess{Microphone: false},
			},
			profile: &Profile{
				Microphone: true,
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Microphone: false},
				},
				Profile: &Profile{
					Microphone: true,
				},
			},
		},
		{
			name: "Microphone OFF: workload ON + profile OFF",
			workload: Workload{
				HostAccess: HostAccess{Microphone: true},
			},
			profile: &Profile{
				Microphone: false,
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Microphone: false},
				},
				Profile: &Profile{
					Microphone: false,
				},
			},
		},
		{
			name: "Microphone OFF: workload OFF + profile OFF",
			workload: Workload{
				HostAccess: HostAccess{Microphone: false},
			},
			profile: &Profile{
				Microphone: false,
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Microphone: false},
				},
				Profile: &Profile{
					Microphone: false,
				},
			},
		},
		{
			name: "Speakers ON: workload ON + profile ON",
			workload: Workload{
				HostAccess: HostAccess{Speakers: true},
			},
			profile: &Profile{
				Speakers: true,
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Speakers: true},
				},
				Profile: &Profile{
					Speakers: true,
				},
			},
		},
		{
			name: "Speakers OFF: workload OFF + profile ON",
			workload: Workload{
				HostAccess: HostAccess{Speakers: false},
			},
			profile: &Profile{
				Speakers: true,
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Speakers: false},
				},
				Profile: &Profile{
					Speakers: true,
				},
			},
		},
		{
			name: "Speakers OFF: workload ON + profile OFF",
			workload: Workload{
				HostAccess: HostAccess{Speakers: true},
			},
			profile: &Profile{
				Speakers: false,
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Speakers: false},
				},
				Profile: &Profile{
					Speakers: false,
				},
			},
		},
		{
			name: "Speakers OFF: workload OFF + profile OFF",
			workload: Workload{
				HostAccess: HostAccess{Speakers: false},
			},
			profile: &Profile{
				Speakers: false,
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Speakers: false},
				},
				Profile: &Profile{
					Speakers: false,
				},
			},
		},
		{
			name: "VarRunUser ON: workload ON + profile ON",
			workload: Workload{
				HostAccess: HostAccess{VarRunUser: true},
			},
			profile: &Profile{
				VarRunUser: true,
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{VarRunUser: true},
				},
				Profile: &Profile{
					VarRunUser: true,
				},
			},
		},
		{
			name: "VarRunUser OFF: workload OFF + profile ON",
			workload: Workload{
				HostAccess: HostAccess{VarRunUser: false},
			},
			profile: &Profile{
				VarRunUser: true,
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{VarRunUser: false},
				},
				Profile: &Profile{
					VarRunUser: true,
				},
			},
		},
		{
			name: "VarRunUser OFF: workload ON + profile OFF",
			workload: Workload{
				HostAccess: HostAccess{VarRunUser: true},
			},
			profile: &Profile{
				VarRunUser: false,
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{VarRunUser: false},
				},
				Profile: &Profile{
					VarRunUser: false,
				},
			},
		},
		{
			name: "VarRunUser OFF: workload OFF + profile OFF",
			workload: Workload{
				HostAccess: HostAccess{VarRunUser: false},
			},
			profile: &Profile{
				VarRunUser: false,
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{VarRunUser: false},
				},
				Profile: &Profile{
					VarRunUser: false,
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
				USBDevices: []string{},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{
						USBDevices: nil,
					},
				},
				Profile: &Profile{
					USBDevices: []string{},
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
				USBDevices: []string{
					"Foo",
					"FooBar",
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
					USBDevices: []string{
						"Foo",
						"FooBar",
					},
				},
			},
		},
		{
			name: "GPUs All: workload all + profile All",
			workload: Workload{
				HostAccess: HostAccess{Gpus: "all"},
			},
			profile: &Profile{
				Gpus: "all",
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Gpus: "all"},
				},
				Profile: &Profile{
					Gpus: "all",
				},
			},
		},
		{
			name: "GPUs empty: workload empty + profile All",
			workload: Workload{
				HostAccess: HostAccess{Gpus: ""},
			},
			profile: &Profile{
				Gpus: "all",
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Gpus: ""},
				},
				Profile: &Profile{
					Gpus: "all",
				},
			},
		},
		{
			name: "GPUs empty: workload all + profile empty",
			workload: Workload{
				HostAccess: HostAccess{Gpus: "all"},
			},
			profile: &Profile{
				Gpus: "",
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Gpus: ""},
				},
				Profile: &Profile{
					Gpus: "",
				},
			},
		},
		{
			name: "GPUs empty: workload empty + profile empty",
			workload: Workload{
				HostAccess: HostAccess{Gpus: ""},
			},
			profile: &Profile{
				Gpus: "",
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Gpus: ""},
				},
				Profile: &Profile{
					Gpus: "",
				},
			},
		},
		{
			name: "Network empty: workload empty + profile empty",
			workload: Workload{
				HostAccess: HostAccess{Network: ""},
			},
			profile: &Profile{
				Network: "",
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Network: ""},
				},
				Profile: &Profile{
					Network: "",
				},
			},
		},
		{
			name: "Network none: workload empty + profile none",
			workload: Workload{
				HostAccess: HostAccess{Network: ""},
			},
			profile: &Profile{
				Network: "none",
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Network: "none"},
				},
				Profile: &Profile{
					Network: "none",
				},
			},
		},
		{
			name: "Network none: workload none + profile empty",
			workload: Workload{
				HostAccess: HostAccess{Network: "none"},
			},
			profile: &Profile{
				Network: "",
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Network: "none"},
				},
				Profile: &Profile{
					Network: "",
				},
			},
		},
		{
			name: "Network foo: workload foo + profile foo",
			workload: Workload{
				HostAccess: HostAccess{Network: "foo"},
			},
			profile: &Profile{
				Network: "foo",
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Network: "foo"},
				},
				Profile: &Profile{
					Network: "foo",
				},
			},
		},
		{
			name: "Network foo: workload empty + profile foo",
			workload: Workload{
				HostAccess: HostAccess{Network: ""},
			},
			profile: &Profile{
				Network: "foo",
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Network: "foo"},
				},
				Profile: &Profile{
					Network: "foo",
				},
			},
		},
		{
			name: "Network empty: workload foo + profile empty",
			workload: Workload{
				HostAccess: HostAccess{Network: "foo"},
			},
			profile: &Profile{
				Network: "",
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Network: ""},
				},
				Profile: &Profile{
					Network: "",
				},
			},
		},
		{
			name: "Network none: workload foo + profile none",
			workload: Workload{
				HostAccess: HostAccess{Network: "none"},
			},
			profile: &Profile{
				Network: "foo",
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Network: "none"},
				},
				Profile: &Profile{
					Network: "foo",
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
				CapsAdd: []string{},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{CapsAdd: []string{}},
				},
				Profile: &Profile{
					CapsAdd: []string{},
				},
			},
		},
		{
			name: "CapsAdd FOO: workload FOO + profile FOO",
			workload: Workload{
				HostAccess: HostAccess{CapsAdd: []string{"FOO"}},
			},
			profile: &Profile{
				CapsAdd: []string{"FOO"},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{CapsAdd: []string{"FOO"}},
				},
				Profile: &Profile{
					CapsAdd: []string{"FOO"},
				},
			},
		},
		{
			name: "CapsAdd empty: workload FOO + profile FOOB",
			workload: Workload{
				HostAccess: HostAccess{CapsAdd: []string{"FOO"}},
			},
			profile: &Profile{
				CapsAdd: []string{"FOOB"},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{CapsAdd: []string{}},
				},
				Profile: &Profile{
					CapsAdd: []string{"FOOB"},
				},
			},
		},
		{
			name: "CapsAdd foo: workload foo + profile foo",
			workload: Workload{
				HostAccess: HostAccess{CapsAdd: []string{"foo"}},
			},
			profile: &Profile{
				CapsAdd: []string{"foo"},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{CapsAdd: []string{"foo"}},
				},
				Profile: &Profile{
					CapsAdd: []string{"foo"},
				},
			},
		},
		{
			name: "CapsAdd bar: workload bar + profile foo and bar",
			workload: Workload{
				HostAccess: HostAccess{CapsAdd: []string{"bar"}},
			},
			profile: &Profile{
				CapsAdd: []string{"foo", "bar"},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{CapsAdd: []string{"bar"}},
				},
				Profile: &Profile{
					CapsAdd: []string{"foo", "bar"},
				},
			},
		},
		{
			name: "CapsAdd empty: workload bar + profile foo",
			workload: Workload{
				HostAccess: HostAccess{CapsAdd: []string{"bar"}},
			},
			profile: &Profile{
				CapsAdd: []string{"foo"},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{CapsAdd: []string{}},
				},
				Profile: &Profile{
					CapsAdd: []string{"foo"},
				},
			},
		},
		{
			name: "Devices empty: workload /dev/foo + profile empty",
			workload: Workload{
				HostAccess: HostAccess{Devices: []string{"/dev/foo"}},
			},
			profile: &Profile{
				Devices: []string{},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Devices: []string{}},
				},
				Profile: &Profile{
					Devices: []string{},
				},
			},
		},
		{
			name: "Devices empty: workload empty + profile /dev/foo",
			workload: Workload{
				HostAccess: HostAccess{Devices: []string{}},
			},
			profile: &Profile{
				Devices: []string{"/dev/foo"},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Devices: []string{}},
				},
				Profile: &Profile{
					Devices: []string{"/dev/foo"},
				},
			},
		},
		{
			name: "Devices /dev/foo: workload /dev/foo + profile /dev/foo",
			workload: Workload{
				HostAccess: HostAccess{Devices: []string{"/dev/foo"}},
			},
			profile: &Profile{
				Devices: []string{"/dev/foo"},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Devices: []string{"/dev/foo"}},
				},
				Profile: &Profile{
					Devices: []string{"/dev/foo"},
				},
			},
		},
		{
			name: "Devices empty: workload /dev/foo/ is not a clean path",
			workload: Workload{
				HostAccess: HostAccess{Devices: []string{"/dev/foo/"}},
			},
			profile: &Profile{
				Devices: []string{"/dev/foo"},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Devices: []string{}},
				},
				Profile: &Profile{
					Devices: []string{"/dev/foo"},
				},
			},
		},
		{
			name: "Devices empty: workload /dev/foo + profile /dev/foob",
			workload: Workload{
				HostAccess: HostAccess{Devices: []string{"/dev/foo"}},
			},
			profile: &Profile{
				Devices: []string{"/dev/foob"},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Devices: []string{}},
				},
				Profile: &Profile{
					Devices: []string{"/dev/foob"},
				},
			},
		},
		{
			name: "Devices /dev/foo: workload /dev/foo + profile /dev/foo",
			workload: Workload{
				HostAccess: HostAccess{Devices: []string{"/dev/foo"}},
			},
			profile: &Profile{
				Devices: []string{"/dev/foo"},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Devices: []string{"/dev/foo"}},
				},
				Profile: &Profile{
					Devices: []string{"/dev/foo"},
				},
			},
		},
		{
			name: "Devices /dev/bar: workload /dev/bar + profile /dev/foo and /dev/bar",
			workload: Workload{
				HostAccess: HostAccess{Devices: []string{"/dev/bar"}},
			},
			profile: &Profile{
				Devices: []string{"/dev/foo", "/dev/bar"},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Devices: []string{"/dev/bar"}},
				},
				Profile: &Profile{
					Devices: []string{"/dev/foo", "/dev/bar"},
				},
			},
		},
		{
			name: "Devices empty: workload /dev/bar + profile /dev/foo",
			workload: Workload{
				HostAccess: HostAccess{Devices: []string{"/dev/bar"}},
			},
			profile: &Profile{
				Devices: []string{"/dev/foo"},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Devices: []string{}},
				},
				Profile: &Profile{
					Devices: []string{"/dev/foo"},
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

func TestWorkloadValidate(t *testing.T) {
	tests := []struct {
		name     string
		workload Workload
		wantErr  bool
	}{
		{
			"gpus: valid all",
			Workload{
				Name:  "valid",
				Image: "valid/valid",
				HostAccess: HostAccess{
					Gpus: "all",
				},
			},
			false,
		},
		{
			"gpus: valid empty",
			Workload{
				Name:  "valid",
				Image: "valid/valid",
				HostAccess: HostAccess{
					Gpus: "",
				},
			},
			false,
		},
		{
			"gpus: invalid foo-bar",
			Workload{
				Name:  "valid",
				Image: "valid/valid",
				HostAccess: HostAccess{
					Gpus: "foo-bar",
				},
			},
			true,
		},
		{
			"command: valid empty",
			Workload{
				Name:    "valid",
				Command: "",
				Image:   "valid/valid",
			},
			false,
		},
		{
			"command: valid 100 len",
			Workload{
				Name:    "valid",
				Command: strings.Repeat("1", 100),
				Image:   "valid/valid",
			},
			false,
		},
		{
			"command: invalid 101 len",
			Workload{
				Name:    "valid",
				Command: strings.Repeat("1", 101),
				Image:   "valid/valid",
			},
			true,
		},
		{
			"image: valid",
			Workload{
				Name:  "valid",
				Image: "test/abc:v1.2.3",
			},
			false,
		},
		{
			"image: invalid empty",
			Workload{
				Name:  "valid",
				Image: "",
			},
			true,
		},
		{
			"runner: valid empty",
			Workload{
				Name:   "valid",
				Image:  "valid/valid",
				Runner: "",
			},
			false,
		},
		{
			"runner: removed docker",
			Workload{
				Name:   "valid",
				Image:  "valid/valid",
				Runner: "docker",
			},
			true,
		},
		{
			"runner: removed podman",
			Workload{
				Name:   "valid",
				Image:  "valid/valid",
				Runner: "podman",
			},
			true,
		},
		{
			"runner: valid firecracker",
			Workload{
				Name:           "valid",
				Image:          "valid/valid",
				Runner:         "firecracker",
				SingleInstance: true,
			},
			false,
		},
		{
			"runner: invalid foobar",
			Workload{
				Name:   "valid",
				Image:  "valid/valid",
				Runner: "foobar",
			},
			true,
		},
		{
			"name: valid",
			Workload{
				Name:  "FOO-bar-321",
				Image: "valid/valid",
			},
			false,
		},
		{
			"name: valid long",
			Workload{
				Name:  strings.Repeat("a", 50),
				Image: "valid/valid",
			},
			false,
		},
		{
			"name: invalid space",
			Workload{
				Name:  "in valid",
				Image: "valid/valid",
			},
			true,
		},
		{
			"name: invalid '",
			Workload{
				Name:  "in'valid",
				Image: "valid/valid",
			},
			true,
		},
		{
			"name: invalid \"",
			Workload{
				Name:  "in\"valid",
				Image: "valid/valid",
			},
			true,
		},
		{
			"name: invalid empty",
			Workload{
				Name:  "",
				Image: "valid/valid",
			},
			true,
		},
		{
			"name: invalid too long",
			Workload{
				Name:  strings.Repeat("a", 51),
				Image: "valid/valid",
			},
			true,
		},
		{
			"devices: valid bare node",
			Workload{
				Name:  "valid",
				Image: "valid/valid",
				HostAccess: HostAccess{
					Devices: []string{"/dev/net/tun"},
				},
			},
			false,
		},
		{
			"devices: invalid remap",
			Workload{
				Name:  "valid",
				Image: "valid/valid",
				HostAccess: HostAccess{
					Devices: []string{"/dev/net/tun:/dev/tun0"},
				},
			},
			true,
		},
		{
			"devices: invalid permissions",
			Workload{
				Name:  "valid",
				Image: "valid/valid",
				HostAccess: HostAccess{
					Devices: []string{"/dev/net/tun:/dev/net/tun:rw"},
				},
			},
			true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.workload.Validate()
			if tc.wantErr && err == nil {
				t.Errorf("expected an error but got nil: %+v", tc.workload)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("did not expect an error but got %v: %+v", err, tc.workload)
			}
		})
	}
}

func TestApplyProfileSeccompUnconfined(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		workload bool
		profile  bool
		want     bool
	}{
		{name: "neither", workload: false, profile: false, want: false},
		{name: "workload only", workload: true, profile: false, want: false},
		{name: "profile only", workload: false, profile: true, want: false},
		{name: "both", workload: true, profile: true, want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := Workload{Name: "w", HostAccess: HostAccess{SeccompUnconfined: tc.workload}}
			p := &Profile{Name: "p", HostAccess: HostAccess{SeccompUnconfined: tc.profile}}

			if got := w.ApplyProfile(p).Workload.HostAccess.SeccompUnconfined; got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// A config naming a runner qubesome no longer has is told it was removed.
// The format error a typo gets reads as if the runner never existed, and
// it is the one that would have been reported here.
func TestWorkloadValidateReportsARemovedRunner(t *testing.T) {
	t.Parallel()

	for _, runner := range []string{"docker", "podman"} {
		w := Workload{Name: "valid", Image: "valid/valid", Runner: runner}

		err := w.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "has been removed")
		assert.Contains(t, err.Error(), runner)
	}
}

func TestWorkloadValidateReportsAnUnknownRunner(t *testing.T) {
	t.Parallel()

	w := Workload{Name: "valid", Image: "valid/valid", Runner: "containerd"}

	err := w.Validate()
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "has been removed")
}

// A bind mount cannot rename a device node and cannot narrow access to
// one. Both are reported when the config is read, rather than when the
// workload is opened.
func TestWorkloadValidateRejectsDeviceRequestsABindCannotHonour(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		device string
		want   string
	}{
		"remapped":   {device: "/dev/kvm:/dev/other", want: "remap"},
		"restricted": {device: "/dev/kvm:/dev/kvm:r", want: "restrict device permissions"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			w := Workload{
				Name:       "valid",
				Image:      "valid/valid",
				HostAccess: HostAccess{Devices: []string{tc.device}},
			}

			err := w.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

// A named network stays valid. 22 entries of the reference configuration
// use one, and the gateway gives them meaning again.
func TestWorkloadValidateAcceptsANamedNetwork(t *testing.T) {
	t.Parallel()

	for _, network := range []string{"", "none", "host", "qubesome"} {
		w := Workload{
			Name:       "valid",
			Image:      "valid/valid",
			HostAccess: HostAccess{Network: network},
		}

		require.NoError(t, w.Validate(), network)
	}
}

// Every other field of a workload is narrowed against the profile's
// allowlist, so the next reader will assume these are too. They are not
// grants: a microVM is a machine shape and attachVM names a sibling
// workload, and neither is something a profile has an opinion on.
func TestApplyProfileLeavesTheMicroVMAlone(t *testing.T) {
	t.Parallel()

	w := Workload{
		Name:           "dev",
		Runner:         "firecracker",
		SingleInstance: true,
		MicroVM: &MicroVM{
			VCPUs:     4,
			MemoryMiB: 8192,
			Data:      &MicroVMData{Path: "/data/dev.ext4", Mount: "/home/dev"},
		},
	}
	console := Workload{Name: "dev-console", AttachVM: "dev"}
	p := &Profile{Name: "personal"}

	assert.Equal(t, w.MicroVM, w.ApplyProfile(p).Workload.MicroVM)
	assert.Equal(t, "dev", console.ApplyProfile(p).Workload.AttachVM)
}

// Limited is the escape hatch, so what matters about it is that it takes
// away and never gives. Everything a workload could reach the host or the
// network through is gone, whatever the config said.
func TestLimitedDropsEverythingThatReachesOut(t *testing.T) {
	t.Parallel()

	ew := EffectiveWorkload{
		Name: "terminal-work",
		Workload: Workload{
			Runner:   "firecracker",
			AttachVM: "dev",
			HostAccess: HostAccess{
				Network:    "qubesome",
				Dbus:       true,
				Camera:     true,
				Microphone: true,
				Speakers:   true,
				Bluetooth:  true,
				VarRunUser: true,
				Mime:       true,
				Gpus:       "all",
				USBDevices: []string{"1050:0407"},
				Devices:    []string{"/dev/dri/renderD128"},
				CapsAdd:    []string{"CAP_NET_ADMIN"},
			},
		},
	}

	got := Limited(ew)

	// "none" and not empty: empty is what a workload that said nothing
	// has, and the profile's network is applied over it.
	assert.Equal(t, "none", got.Workload.HostAccess.Network)
	assert.False(t, GatewayNetwork(got.Workload.HostAccess.Network))

	assert.False(t, got.Workload.HostAccess.Dbus)
	assert.False(t, got.Workload.HostAccess.Camera)
	assert.False(t, got.Workload.HostAccess.Microphone)
	assert.False(t, got.Workload.HostAccess.Speakers)
	assert.False(t, got.Workload.HostAccess.Bluetooth)
	assert.False(t, got.Workload.HostAccess.VarRunUser)
	assert.False(t, got.Workload.HostAccess.Mime)
	assert.Empty(t, got.Workload.HostAccess.Gpus)
	assert.Empty(t, got.Workload.HostAccess.USBDevices)
	assert.Empty(t, got.Workload.HostAccess.Devices)
	assert.Empty(t, got.Workload.HostAccess.CapsAdd)

	// A machine is unreachable when the gateway is what is broken, and a
	// guest is not somewhere the host's profile can be looked at from.
	assert.Empty(t, got.Workload.Runner)
	assert.Empty(t, got.Workload.AttachVM)
}

// The paths are what the config being repaired is reached through. A
// rescue shell that cannot see the file it was opened to fix is not one.
func TestLimitedKeepsTheMappedPaths(t *testing.T) {
	t.Parallel()

	ew := EffectiveWorkload{
		Workload: Workload{
			HostAccess: HostAccess{Paths: []string{"${HOME}/git:/git"}},
		},
	}

	assert.Equal(t, []string{"${HOME}/git:/git"}, Limited(ew).Workload.HostAccess.Paths)
}
