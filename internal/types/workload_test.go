package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_ApplyProfile(t *testing.T) {
	tests := []struct {
		name     string
		workload Workload
		profile  Profile
		want     EffectiveWorkload
	}{
		{
			name: "Camera ON: workload ON + profile ON",
			workload: Workload{
				HostAccess: HostAccess{Camera: true},
			},
			profile: Profile{
				HostAccess: HostAccess{Camera: true},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Camera: true},
				},
				Profile: Profile{
					HostAccess: HostAccess{Camera: true},
				},
			},
		},
		{
			name: "Camera OFF: workload OFF + profile ON",
			workload: Workload{
				HostAccess: HostAccess{Camera: false},
			},
			profile: Profile{
				HostAccess: HostAccess{Camera: true},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Camera: false},
				},
				Profile: Profile{
					HostAccess: HostAccess{Camera: true},
				},
			},
		},
		{
			name: "Camera OFF: workload ON + profile OFF",
			workload: Workload{
				HostAccess: HostAccess{Camera: true},
			},
			profile: Profile{
				HostAccess: HostAccess{Camera: false},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Camera: false},
				},
				Profile: Profile{
					HostAccess: HostAccess{Camera: false},
				},
			},
		},
		{
			name: "Camera OFF: workload OFF + profile OFF",
			workload: Workload{
				HostAccess: HostAccess{Camera: false},
			},
			profile: Profile{
				HostAccess: HostAccess{Camera: false},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Camera: false},
				},
				Profile: Profile{
					HostAccess: HostAccess{Camera: false},
				},
			},
		},
		{
			name: "X11 ON: workload ON + profile ON",
			workload: Workload{
				HostAccess: HostAccess{X11: true},
			},
			profile: Profile{
				HostAccess: HostAccess{X11: true},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{X11: true},
				},
				Profile: Profile{
					HostAccess: HostAccess{X11: true},
				},
			},
		},
		{
			name: "X11 OFF: workload OFF + profile ON",
			workload: Workload{
				HostAccess: HostAccess{X11: false},
			},
			profile: Profile{
				HostAccess: HostAccess{X11: true},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{X11: false},
				},
				Profile: Profile{
					HostAccess: HostAccess{X11: true},
				},
			},
		},
		{
			name: "X11 OFF: workload ON + profile OFF",
			workload: Workload{
				HostAccess: HostAccess{X11: true},
			},
			profile: Profile{
				HostAccess: HostAccess{X11: false},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{X11: false},
				},
				Profile: Profile{
					HostAccess: HostAccess{X11: false},
				},
			},
		},
		{
			name: "X11 OFF: workload OFF + profile OFF",
			workload: Workload{
				HostAccess: HostAccess{X11: false},
			},
			profile: Profile{
				HostAccess: HostAccess{X11: false},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{X11: false},
				},
				Profile: Profile{
					HostAccess: HostAccess{X11: false},
				},
			},
		},
		{
			name: "Microphone ON: workload ON + profile ON",
			workload: Workload{
				HostAccess: HostAccess{Microphone: true},
			},
			profile: Profile{
				HostAccess: HostAccess{Microphone: true},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Microphone: true},
				},
				Profile: Profile{
					HostAccess: HostAccess{Microphone: true},
				},
			},
		},
		{
			name: "Microphone OFF: workload OFF + profile ON",
			workload: Workload{
				HostAccess: HostAccess{Microphone: false},
			},
			profile: Profile{
				HostAccess: HostAccess{Microphone: true},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Microphone: false},
				},
				Profile: Profile{
					HostAccess: HostAccess{Microphone: true},
				},
			},
		},
		{
			name: "Microphone OFF: workload ON + profile OFF",
			workload: Workload{
				HostAccess: HostAccess{Microphone: true},
			},
			profile: Profile{
				HostAccess: HostAccess{Microphone: false},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Microphone: false},
				},
				Profile: Profile{
					HostAccess: HostAccess{Microphone: false},
				},
			},
		},
		{
			name: "Microphone OFF: workload OFF + profile OFF",
			workload: Workload{
				HostAccess: HostAccess{Microphone: false},
			},
			profile: Profile{
				HostAccess: HostAccess{Microphone: false},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Microphone: false},
				},
				Profile: Profile{
					HostAccess: HostAccess{Microphone: false},
				},
			},
		},
		{
			name: "Speakers ON: workload ON + profile ON",
			workload: Workload{
				HostAccess: HostAccess{Speakers: true},
			},
			profile: Profile{
				HostAccess: HostAccess{Speakers: true},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Speakers: true},
				},
				Profile: Profile{
					HostAccess: HostAccess{Speakers: true},
				},
			},
		},
		{
			name: "Speakers OFF: workload OFF + profile ON",
			workload: Workload{
				HostAccess: HostAccess{Speakers: false},
			},
			profile: Profile{
				HostAccess: HostAccess{Speakers: true},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Speakers: false},
				},
				Profile: Profile{
					HostAccess: HostAccess{Speakers: true},
				},
			},
		},
		{
			name: "Speakers OFF: workload ON + profile OFF",
			workload: Workload{
				HostAccess: HostAccess{Speakers: true},
			},
			profile: Profile{
				HostAccess: HostAccess{Speakers: false},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Speakers: false},
				},
				Profile: Profile{
					HostAccess: HostAccess{Speakers: false},
				},
			},
		},
		{
			name: "Speakers OFF: workload OFF + profile OFF",
			workload: Workload{
				HostAccess: HostAccess{Speakers: false},
			},
			profile: Profile{
				HostAccess: HostAccess{Speakers: false},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Speakers: false},
				},
				Profile: Profile{
					HostAccess: HostAccess{Speakers: false},
				},
			},
		},
		{
			name: "Smartcard ON: workload ON + profile ON",
			workload: Workload{
				HostAccess: HostAccess{Smartcard: true},
			},
			profile: Profile{
				HostAccess: HostAccess{Smartcard: true},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Smartcard: true},
				},
				Profile: Profile{
					HostAccess: HostAccess{Smartcard: true},
				},
			},
		},
		{
			name: "Smartcard OFF: workload OFF + profile ON",
			workload: Workload{
				HostAccess: HostAccess{Smartcard: false},
			},
			profile: Profile{
				HostAccess: HostAccess{Smartcard: true},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Smartcard: false},
				},
				Profile: Profile{
					HostAccess: HostAccess{Smartcard: true},
				},
			},
		},
		{
			name: "Smartcard OFF: workload ON + profile OFF",
			workload: Workload{
				HostAccess: HostAccess{Smartcard: true},
			},
			profile: Profile{
				HostAccess: HostAccess{Smartcard: false},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Smartcard: false},
				},
				Profile: Profile{
					HostAccess: HostAccess{Smartcard: false},
				},
			},
		},
		{
			name: "Smartcard OFF: workload OFF + profile OFF",
			workload: Workload{
				HostAccess: HostAccess{Smartcard: false},
			},
			profile: Profile{
				HostAccess: HostAccess{Smartcard: false},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{Smartcard: false},
				},
				Profile: Profile{
					HostAccess: HostAccess{Smartcard: false},
				},
			},
		},

		{
			name: "HostName ON: workload ON + profile ON",
			workload: Workload{
				HostAccess: HostAccess{HostName: true},
			},
			profile: Profile{
				HostAccess: HostAccess{HostName: true},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{HostName: true},
				},
				Profile: Profile{
					HostAccess: HostAccess{HostName: true},
				},
			},
		},
		{
			name: "HostName OFF: workload OFF + profile ON",
			workload: Workload{
				HostAccess: HostAccess{HostName: false},
			},
			profile: Profile{
				HostAccess: HostAccess{HostName: true},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{HostName: false},
				},
				Profile: Profile{
					HostAccess: HostAccess{HostName: true},
				},
			},
		},
		{
			name: "HostName OFF: workload ON + profile OFF",
			workload: Workload{
				HostAccess: HostAccess{HostName: true},
			},
			profile: Profile{
				HostAccess: HostAccess{HostName: false},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{HostName: false},
				},
				Profile: Profile{
					HostAccess: HostAccess{HostName: false},
				},
			},
		},
		{
			name: "HostName OFF: workload OFF + profile OFF",
			workload: Workload{
				HostAccess: HostAccess{HostName: false},
			},
			profile: Profile{
				HostAccess: HostAccess{HostName: false},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{HostName: false},
				},
				Profile: Profile{
					HostAccess: HostAccess{HostName: false},
				},
			},
		},
		{
			name: "VarRunUser ON: workload ON + profile ON",
			workload: Workload{
				HostAccess: HostAccess{VarRunUser: true},
			},
			profile: Profile{
				HostAccess: HostAccess{VarRunUser: true},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{VarRunUser: true},
				},
				Profile: Profile{
					HostAccess: HostAccess{VarRunUser: true},
				},
			},
		},
		{
			name: "VarRunUser OFF: workload OFF + profile ON",
			workload: Workload{
				HostAccess: HostAccess{VarRunUser: false},
			},
			profile: Profile{
				HostAccess: HostAccess{VarRunUser: true},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{VarRunUser: false},
				},
				Profile: Profile{
					HostAccess: HostAccess{VarRunUser: true},
				},
			},
		},
		{
			name: "VarRunUser OFF: workload ON + profile OFF",
			workload: Workload{
				HostAccess: HostAccess{VarRunUser: true},
			},
			profile: Profile{
				HostAccess: HostAccess{VarRunUser: false},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{VarRunUser: false},
				},
				Profile: Profile{
					HostAccess: HostAccess{VarRunUser: false},
				},
			},
		},
		{
			name: "VarRunUser OFF: workload OFF + profile OFF",
			workload: Workload{
				HostAccess: HostAccess{VarRunUser: false},
			},
			profile: Profile{
				HostAccess: HostAccess{VarRunUser: false},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					HostAccess: HostAccess{VarRunUser: false},
				},
				Profile: Profile{
					HostAccess: HostAccess{VarRunUser: false},
				},
			},
		},
		{
			name: "NamedDevices: drop named devices not in profile",
			workload: Workload{
				NamedDevices: []string{"Foo and Bar"},
			},
			profile: Profile{
				NamedDevices: []string{},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					NamedDevices: nil,
				},
				Profile: Profile{
					NamedDevices: []string{},
				},
			},
		},
		{
			name: "NamedDevices: add allowed named devices",
			workload: Workload{
				NamedDevices: []string{
					"Foo and Bar",
					"Foo",
					"Bar",
				},
			},
			profile: Profile{
				NamedDevices: []string{
					"Foo",
					"FooBar",
				},
			},
			want: EffectiveWorkload{
				Name: "-",
				Workload: Workload{
					NamedDevices: []string{
						"Foo",
					},
				},
				Profile: Profile{
					NamedDevices: []string{
						"Foo",
						"FooBar",
					},
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
