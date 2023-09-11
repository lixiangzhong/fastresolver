package fastresolver

type ServerRefusedError struct {
	Qname  string
	Server string
}

func (e ServerRefusedError) Error() string {
	return "server " + e.Server + " refused query for " + e.Qname
}
