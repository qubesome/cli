package launch

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/sys/execabs"
)

func TestRecordedPID(t *testing.T) {
	t.Parallel()

	started := &execabs.Cmd{Process: &os.Process{Pid: 100}}

	tests := []struct {
		name string
		l    *Launcher
		want int
	}{
		{name: "nested launch names the sandbox init", l: &Launcher{cmd: started, pid: 200}, want: 200},
		{name: "plain launch names the process started", l: &Launcher{cmd: started}, want: 100},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.l.RecordedPID())
		})
	}
}
