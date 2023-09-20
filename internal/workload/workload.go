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

func (e *Effective) Validate() error {
	//TODO: validation
	return nil
}

type Opts struct {
	Camera     bool
	Speakers   bool
	Microphone bool
	X11        bool
	SmartCard  bool
	Network    string
	VarRunUser bool // TODO: improve abstraction
}
