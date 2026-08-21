package types

import (
	"strings"
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
			name: "Dbus full: workload full + profile full",
			workload: Workload{
				HostAccess: HostAccess{Dbus: DbusPolicy{Mode: DbusFull}},
			},
			profile: &Profile{
				HostAccess: HostAccess{Dbus: DbusPolicy{Mode: DbusFull}},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Dbus: DbusPolicy{Mode: DbusFull}},
				},
				Profile: &Profile{
					HostAccess: HostAccess{Dbus: DbusPolicy{Mode: DbusFull}},
				},
			},
		},
		{
			name: "Dbus none: workload none + profile full",
			workload: Workload{
				HostAccess: HostAccess{Dbus: DbusPolicy{Mode: DbusNone}},
			},
			profile: &Profile{
				HostAccess: HostAccess{Dbus: DbusPolicy{Mode: DbusFull}},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Dbus: DbusPolicy{Mode: DbusNone}},
				},
				Profile: &Profile{
					HostAccess: HostAccess{Dbus: DbusPolicy{Mode: DbusFull}},
				},
			},
		},
		{
			name: "Dbus filtered: profile bounds workload rules",
			workload: Workload{
				HostAccess: HostAccess{Dbus: DbusPolicy{
					Mode:    DbusFiltered,
					Session: []string{"talk=org.foo.Bar", "talk=org.denied"},
				}},
			},
			profile: &Profile{
				HostAccess: HostAccess{Dbus: DbusPolicy{
					Mode:    DbusFiltered,
					Session: []string{"talk=org.foo.*"},
				}},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Dbus: DbusPolicy{
						Mode:    DbusFiltered,
						Session: []string{"talk=org.foo.Bar"},
					}},
				},
				Profile: &Profile{
					HostAccess: HostAccess{Dbus: DbusPolicy{
						Mode:    DbusFiltered,
						Session: []string{"talk=org.foo.*"},
					}},
				},
			},
		},
		{
			name: "Dbus none: workload full + profile none",
			workload: Workload{
				HostAccess: HostAccess{Dbus: DbusPolicy{Mode: DbusFull}},
			},
			profile: &Profile{
				HostAccess: HostAccess{Dbus: DbusPolicy{Mode: DbusNone}},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Dbus: DbusPolicy{Mode: DbusNone}},
				},
				Profile: &Profile{
					HostAccess: HostAccess{Dbus: DbusPolicy{Mode: DbusNone}},
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
			name: "Privileged ON: workload ON + profile ON",
			workload: Workload{
				HostAccess: HostAccess{Privileged: true},
			},
			profile: &Profile{
				Privileged: true,
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Privileged: true},
				},
				Profile: &Profile{
					Privileged: true,
				},
			},
		},
		{
			name: "Privileged OFF: workload OFF + profile ON",
			workload: Workload{
				HostAccess: HostAccess{Privileged: false},
			},
			profile: &Profile{
				Privileged: true,
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Privileged: false},
				},
				Profile: &Profile{
					Privileged: true,
				},
			},
		},
		{
			name: "Privileged OFF: workload ON + profile OFF",
			workload: Workload{
				HostAccess: HostAccess{Privileged: true},
			},
			profile: &Profile{
				Privileged: false,
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Privileged: false},
				},
				Profile: &Profile{
					Privileged: false,
				},
			},
		},
		{
			name: "Privileged OFF: workload OFF + profile OFF",
			workload: Workload{
				HostAccess: HostAccess{Privileged: false},
			},
			profile: &Profile{
				Privileged: false,
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Privileged: false},
				},
				Profile: &Profile{
					Privileged: false,
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
			name: "Devices empty: workload /foo + profile empty",
			workload: Workload{
				HostAccess: HostAccess{Devices: []string{"/foo"}},
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
			name: "Devices empty: workload empty + profile /foo",
			workload: Workload{
				HostAccess: HostAccess{Devices: []string{}},
			},
			profile: &Profile{
				Devices: []string{"/foo"},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Devices: []string{}},
				},
				Profile: &Profile{
					Devices: []string{"/foo"},
				},
			},
		},
		{
			name: "Devices /foo: workload /foo + profile /foo",
			workload: Workload{
				HostAccess: HostAccess{Devices: []string{"/foo"}},
			},
			profile: &Profile{
				Devices: []string{"/foo"},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Devices: []string{"/foo"}},
				},
				Profile: &Profile{
					Devices: []string{"/foo"},
				},
			},
		},
		{
			name: "Devices /foo/: workload /foo/ + profile /foo",
			workload: Workload{
				HostAccess: HostAccess{Devices: []string{"/foo/"}},
			},
			profile: &Profile{
				Devices: []string{"/foo"},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Devices: []string{"/foo/"}},
				},
				Profile: &Profile{
					Devices: []string{"/foo"},
				},
			},
		},
		{
			name: "Devices empty: workload /foo + profile /foob",
			workload: Workload{
				HostAccess: HostAccess{Devices: []string{"/foo"}},
			},
			profile: &Profile{
				Devices: []string{"/foob"},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Devices: []string{}},
				},
				Profile: &Profile{
					Devices: []string{"/foob"},
				},
			},
		},
		{
			name: "Devices /foo: workload /foo + profile /foo",
			workload: Workload{
				HostAccess: HostAccess{Devices: []string{"/foo"}},
			},
			profile: &Profile{
				Devices: []string{"/foo"},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Devices: []string{"/foo"}},
				},
				Profile: &Profile{
					Devices: []string{"/foo"},
				},
			},
		},
		{
			name: "Devices /bar: workload /bar + profile /foo and /bar",
			workload: Workload{
				HostAccess: HostAccess{Devices: []string{"/bar"}},
			},
			profile: &Profile{
				Devices: []string{"/foo", "/bar"},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Devices: []string{"/bar"}},
				},
				Profile: &Profile{
					Devices: []string{"/foo", "/bar"},
				},
			},
		},
		{
			name: "Devices empty: workload /bar + profile /foo",
			workload: Workload{
				HostAccess: HostAccess{Devices: []string{"/bar"}},
			},
			profile: &Profile{
				Devices: []string{"/foo"},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Devices: []string{}},
				},
				Profile: &Profile{
					Devices: []string{"/foo"},
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
			"runner: valid docker",
			Workload{
				Name:   "valid",
				Image:  "valid/valid",
				Runner: "docker",
			},
			false,
		},
		{
			"runner: valid firecracker",
			Workload{
				Name:   "valid",
				Image:  "valid/valid",
				Runner: "firecracker",
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
