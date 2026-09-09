package sandbox

import (
	"flag"
	"os"
	"testing"
)

var update = flag.Bool("update", false, "update the golden files")

func TestMain(m *testing.M) {
	flag.Parse()
	os.Exit(m.Run())
}
