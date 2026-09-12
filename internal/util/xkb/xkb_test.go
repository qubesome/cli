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

// setxkbmap asks the X server. On a Wayland session that is Xwayland,
// which carries its own default rather than the compositor's keymap, and
// the first host this ran on reported us while typing gb.
func TestFromLocalectl(t *testing.T) {
	t.Parallel()

	const status = `   System Locale: LANG=en_GB.UTF-8
       VC Keymap: uk
      X11 Layout: gb
       X11 Model: pc105
     X11 Options: terminate:ctrl_alt_bksp
`

	got := fromLocalectl(func() ([]byte, error) { return []byte(status), nil })

	require.Equal(t, []string{
		"XKB_DEFAULT_MODEL=pc105",
		"XKB_DEFAULT_LAYOUT=gb",
		"XKB_DEFAULT_OPTIONS=terminate:ctrl_alt_bksp",
	}, got)
}

// The VC keymap names a console keymap and not an XKB layout, so a
// status carrying only that one is not a keymap this can use.
func TestFromLocalectlIgnoresTheConsoleKeymap(t *testing.T) {
	t.Parallel()

	require.Empty(t, fromLocalectl(func() ([]byte, error) {
		return []byte("       VC Keymap: uk\n"), nil
	}))
}

func TestFromLocalectlFailingLeavesTheDefaultAlone(t *testing.T) {
	t.Parallel()

	require.Empty(t, fromLocalectl(func() ([]byte, error) {
		return nil, errors.New("not found")
	}))
}

func TestFirstOfTakesTheFirstThatAnswered(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{"b"}, firstOf(nil, []string{"b"}, []string{"c"}))
	require.Empty(t, firstOf(nil, nil))
}

// Which of the two answers to believe depends on the session, because the
// two tools are reliable on opposite ones. On X11 setxkbmap reports the
// keymap the user is typing on, including one applied by hand after login.
// On Wayland it reports Xwayland's own default, which is nobody's layout.
func TestPreferredSource(t *testing.T) {
	t.Parallel()

	const live = "rules: evdev\nmodel: pc105\nlayout: gb\n"
	const configured = "X11 Layout: us\nX11 Model: pc104\n"

	tests := []struct {
		name    string
		session string
		want    string
	}{
		{"x11 prefers the running layout", "x11", "XKB_DEFAULT_LAYOUT=gb"},
		{"a session that says nothing is treated as x11", "", "XKB_DEFAULT_LAYOUT=gb"},
		{"wayland prefers the configured layout", "wayland", "XKB_DEFAULT_LAYOUT=us"},
		{"wayland is matched whatever its case", "Wayland", "XKB_DEFAULT_LAYOUT=us"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := preferred(tc.session,
				func() ([]byte, error) { return []byte(live), nil },
				func() ([]byte, error) { return []byte(configured), nil },
			)

			require.Contains(t, got, tc.want)
		})
	}
}

// Whichever is preferred, the other still answers when the first cannot.
func TestPreferredFallsBack(t *testing.T) {
	t.Parallel()

	const configured = "X11 Layout: us\n"
	fails := func() ([]byte, error) { return nil, errors.New("not found") }

	got := preferred("x11", fails, func() ([]byte, error) { return []byte(configured), nil })
	require.Contains(t, got, "XKB_DEFAULT_LAYOUT=us")

	got = preferred("wayland", func() ([]byte, error) { return []byte("layout: gb\n"), nil }, fails)
	require.Contains(t, got, "XKB_DEFAULT_LAYOUT=gb")
}
