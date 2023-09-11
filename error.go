package fastresolver

type ServerRefusedError struct {
	Qname  string
	Server string
}

func (e ServerRefusedError) Error() string {
	return "server " + e.Server + " refused query for " + e.Qname
}

type TruncatedError struct {
	Qname  string
	Server string
}

func (e TruncatedError) Error() string {
	return "server " + e.Server + " truncated query for " + e.Qname
}
