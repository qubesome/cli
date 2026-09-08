package xkb

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFromSetxkbmap(t *testing.T) {
	t.Parallel()

	t.Run("a full query becomes every variable", func(t *testing.T) {
		t.Parallel()

		got := fromSetxkbmap(func() ([]byte, error) {
			return []byte("rules:      evdev\nmodel:      pc105\nlayout:     gb\nvariant:    dvorak\noptions:    terminate:ctrl_alt_bksp\n"), nil
		})

		require.Equal(t, []string{
			"XKB_DEFAULT_RULES=evdev",
			"XKB_DEFAULT_MODEL=pc105",
			"XKB_DEFAULT_LAYOUT=gb",
			"XKB_DEFAULT_VARIANT=dvorak",
			"XKB_DEFAULT_OPTIONS=terminate:ctrl_alt_bksp",
		}, got)
	})

	t.Run("empty components are left out", func(t *testing.T) {
		t.Parallel()

		got := fromSetxkbmap(func() ([]byte, error) {
			return []byte("rules:      evdev\nmodel:      pc105\nlayout:     us\nvariant:\noptions:\n"), nil
		})

		require.Equal(t, []string{
			"XKB_DEFAULT_RULES=evdev",
			"XKB_DEFAULT_MODEL=pc105",
			"XKB_DEFAULT_LAYOUT=us",
		}, got)
	})

	t.Run("no layout is no keymap", func(t *testing.T) {
		t.Parallel()

		require.Empty(t, fromSetxkbmap(func() ([]byte, error) {
			return []byte("rules:      evdev\nmodel:      pc105\n"), nil
		}))
	})

	t.Run("a command that fails leaves the default alone", func(t *testing.T) {
		t.Parallel()

		require.Empty(t, fromSetxkbmap(func() ([]byte, error) {
			return nil, errors.New("not found")
		}))
	})

	// The values become --setenv arguments, so anything that could end an
	// entry or begin another is dropped rather than passed on.
	t.Run("a value that is not a keymap is dropped", func(t *testing.T) {
		t.Parallel()

		require.Equal(t, []string{"XKB_DEFAULT_LAYOUT=gb"},
			fromSetxkbmap(func() ([]byte, error) {
				return []byte("layout: gb\nvariant: a b\noptions: x=1\n"), nil
			}))
	})

	// A layout that is not one takes the whole keymap with it, since the
	// rest describes how to interpret a layout there is now none of.
	t.Run("a hostile layout leaves the default alone", func(t *testing.T) {
		t.Parallel()

		require.Empty(t, fromSetxkbmap(func() ([]byte, error) {
			return []byte("layout: gb\nPATH=/tmp\n"), nil
		})[1:])
	})
}

func TestFromEnv(t *testing.T) {
	t.Run("a layout is enough", func(t *testing.T) {
		t.Setenv("XKB_DEFAULT_LAYOUT", "gb")

		require.Equal(t, []string{"XKB_DEFAULT_LAYOUT=gb"}, fromEnv())
	})

	t.Run("options without a layout are not a keymap", func(t *testing.T) {
		t.Setenv("XKB_DEFAULT_OPTIONS", "caps:escape")

		require.Empty(t, fromEnv())
	})

	t.Run("a hostile value is dropped", func(t *testing.T) {
		t.Setenv("XKB_DEFAULT_LAYOUT", "gb\nPATH=/tmp")

		require.Empty(t, fromEnv())
	})
}
