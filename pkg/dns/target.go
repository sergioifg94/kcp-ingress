package dns

const (
	TargetTypeHost = "HOST"
	TargetTypeIP   = "IP"
)

type Target struct {
	Cluster    string
	TargetType string
	Value      string
}
