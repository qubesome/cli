package clipboard_test

import (
	"testing"

	cmd "github.com/qubesome/cli/cmd/clipboard"
	"github.com/qubesome/cli/internal/clipboard"
	"github.com/qubesome/cli/internal/command"
	"github.com/qubesome/cli/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const usage = `usage:
    %[1]s clipboard --from-profile <profile_name> <profile_to>
    %[1]s clipboard --type image/png --from-host <profile_to>
    %[1]s clipboard --from-host <profile_to>
`

type consoleMock = *command.ConsoleMock[clipboard.Options]
type handlerMock = *command.HandlerMock[clipboard.Options]

func TestCommand(t *testing.T) {
	tests := []struct {
		name      string
		mockSetup func(consoleMock, handlerMock)
		action    command.Action[clipboard.Options]
		opts      interface{}
		err       string
	}{
		{
			name: "empty",
			mockSetup: func(cm consoleMock, hm handlerMock) {
				cm.On("Args").Return([]string{})
				cm.On("Usage", usage)
			},
			opts: &clipboard.Options{},
		},
		{
			name: "no source set",
			mockSetup: func(cm consoleMock, hm handlerMock) {
				cm.On("Args").Return([]string{"profile"})
				cm.On("Usage", usage)
			},
			opts: &clipboard.Options{},
		},
		{
			name: "both --from-host and --from-profile",
			mockSetup: func(cm consoleMock, hm handlerMock) {
				cm.On("Args").Return([]string{"--from-profile", "foo", "--from-host", "profile"})
				cm.On("Usage", usage)
			},
			opts: &clipboard.Options{},
		},
		{
			name: "--from-host foo",
			mockSetup: func(cm consoleMock, hm handlerMock) {
				cm.On("Args").Return([]string{"--from-host", "foo"})
				cm.On("UserConfig").Return(&types.Config{
					Profiles: map[string]*types.Profile{
						"foo": {Name: "foo"},
					},
				})
				hm.On("Run", []command.Option[clipboard.Options](nil)).Return(nil)
			},
			action: cmd.New().(command.Action[clipboard.Options]),
			opts: &clipboard.Options{
				FromHost:      true,
				TargetProfile: &types.Profile{Name: "foo"},
			},
		},
		{
			name: "--from-host --type images/jpeg foo",
			mockSetup: func(cm consoleMock, hm handlerMock) {
				cm.On("Args").Return([]string{"--from-host", "--type", "images/jpeg", "foo"})
				cm.On("UserConfig").Return(&types.Config{
					Profiles: map[string]*types.Profile{
						"foo": {Name: "foo"},
					},
				})
				hm.On("Run", []command.Option[clipboard.Options](nil)).Return(nil)
			},
			action: cmd.New().(command.Action[clipboard.Options]),
			opts: &clipboard.Options{
				FromHost:      true,
				TargetProfile: &types.Profile{Name: "foo"},
				ContentType:   "images/jpeg",
			},
		},
		{
			name: "--from-profile foo bar",
			mockSetup: func(cm consoleMock, hm handlerMock) {
				cm.On("Args").Return([]string{"--from-profile", "foo", "bar"})
				cm.On("UserConfig").Return(&types.Config{
					Profiles: map[string]*types.Profile{
						"foo": {Name: "foo"},
						"bar": {Name: "bar"},
					},
				})
				hm.On("Run", []command.Option[clipboard.Options](nil)).Return(nil)
			},
			action: cmd.New().(command.Action[clipboard.Options]),
			opts: &clipboard.Options{
				SourceProfile: &types.Profile{Name: "foo"},
				TargetProfile: &types.Profile{Name: "bar"},
			},
		},
		{
			name: "missing source profile",
			mockSetup: func(cm consoleMock, hm handlerMock) {
				cm.On("Args").Return([]string{"--from-profile", "foo", "bar"})
				cm.On("UserConfig").Return(&types.Config{
					Profiles: map[string]*types.Profile{
						"bar": {Name: "bar"},
					},
				})
				hm.On("Run", []command.Option[clipboard.Options](nil)).Return(nil)
			},
			action: cmd.New().(command.Action[clipboard.Options]),
			opts: &clipboard.Options{
				SourceProfile: &types.Profile{Name: "foo"},
				TargetProfile: &types.Profile{Name: "bar"},
			},
			err: "source profile foo not found",
		},
		{
			name: "missing target profile",
			mockSetup: func(cm consoleMock, hm handlerMock) {
				cm.On("Args").Return([]string{"--from-profile", "foo", "bar"})
				cm.On("UserConfig").Return(&types.Config{
					Profiles: map[string]*types.Profile{
						"foo": {Name: "foo"},
					},
				})
				hm.On("Run", []command.Option[clipboard.Options](nil)).Return(nil)
			},
			action: cmd.New().(command.Action[clipboard.Options]),
			opts: &clipboard.Options{
				SourceProfile: &types.Profile{Name: "foo"},
				TargetProfile: &types.Profile{Name: "bar"},
			},
			err: "target profile bar not found",
		},
		{
			name: "[profile config] --from-profile foo bar",
			mockSetup: func(cm consoleMock, hm handlerMock) {
				cm.On("Args").Return([]string{"--from-profile", "foo", "bar"})
				cm.On("UserConfig").Return(nil)
				cm.On("ProfileConfig", "bar").Return(&types.Config{
					Profiles: map[string]*types.Profile{
						"foo": {Name: "foo"},
						"bar": {Name: "bar"},
					},
				})
				hm.On("Run", []command.Option[clipboard.Options](nil)).Return(nil)
			},
			action: cmd.New().(command.Action[clipboard.Options]),
			opts: &clipboard.Options{
				SourceProfile: &types.Profile{Name: "foo"},
				TargetProfile: &types.Profile{Name: "bar"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cm := new(command.ConsoleMock[clipboard.Options])
			hm := command.NewHandlerMock(cmd.New().Handle)
			tc.mockSetup(cm, hm)

			action, opts, err := hm.Handle(cm)
			if tc.err == "" {
				require.NoError(t, err)

				assert.Equal(t, tc.action, action)

				o := &clipboard.Options{}
				for _, opt := range opts {
					opt(o)
				}

				assert.Equal(t, tc.opts, o)
			} else {
				require.ErrorContains(t, err, tc.err)
			}

			cm.AssertExpectations(t)
		})
	}
}
