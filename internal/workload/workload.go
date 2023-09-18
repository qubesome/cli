package workload

type Effective struct {
	Name           string
	Profile        string
	Image          string
	Command        string
	Args           []string
	SingleInstance bool
	Opts
	Path         []string
	NamedDevices []string
}

type WorkloadDefault struct {
	Opts
}

type WorkloadInstance struct {
	Opts
}

type Opts struct {
	Camera    bool
	Audio     bool
	X11       bool
	SmartCard bool
}
