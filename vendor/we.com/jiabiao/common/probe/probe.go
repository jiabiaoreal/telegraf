package probe

type Result string

const (
	Success Result = "green"
	Warning Result = "yellow"
	Failure Result = "red"
	Unknown Result = "unknown"
)

type LoadGenerator func() interface{}

type Prober interface {
	Prob(lg LoadGenerator) (Result, string, error)
}
