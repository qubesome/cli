package images

import (
	"testing"

	"github.com/qubesome/cli/internal/types"
	"github.com/stretchr/testify/assert"
)

func TestProfileImages(t *testing.T) {
	t.Parallel()

	cfg := &types.Config{
		Profiles: map[string]types.Profile{
			"work":     {Image: "ghcr.io/qubesome/xorg:latest"},
			"personal": {Image: "ghcr.io/qubesome/xorg:latest"},
			"pentest":  {Image: "ghcr.io/qubesome/kali:latest"},
			"noimage":  {},
		},
	}

	assert.ElementsMatch(t,
		[]string{"ghcr.io/qubesome/xorg:latest", "ghcr.io/qubesome/kali:latest"},
		ProfileImages(cfg))
}

func TestProfileImagesNilConfig(t *testing.T) {
	t.Parallel()

	assert.Empty(t, ProfileImages(nil))
}
