package website

type StatusView struct {
	Healthy bool

	// health
	TenantCount string
	DiskUsed    string
	Uptime      string

	// benchmark
	WriteP50  string
	WriteP99  string
	ReadP50   string
	ReadP99   string
	OpsPerSec string
	TotalOps  string

	RecentOps []StatusOp
}

type StatusOp struct {
	When    string
	Op      string
	Latency string
	OK      bool
}

func (o StatusOp) StatusClass() string {
	if o.OK {
		return "status-ok"
	}
	return "status-err"
}

func (o StatusOp) StatusText() string {
	if o.OK {
		return "ok"
	}
	return "err"
}
