package types

type WorkloadPullMode string

const (
	// OnDemand is a no-op and won't preemptively pull workload images.
	// This is the default behaviour.
	OnDemand WorkloadPullMode = "on-demand"
	// Background downloads all workload images on the background when
	// any command is executed. This operation will only take place once
	// a day.
	Background WorkloadPullMode = "background"
)
