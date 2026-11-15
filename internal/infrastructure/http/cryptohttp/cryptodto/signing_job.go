package cryptodto

// SigningJobStatusResponse exposes a safe operational summary of an approval
// signing job without the frozen intent or signed material.
type SigningJobStatusResponse struct {
	Status    string `json:"status"`
	Attempts  int    `json:"attempts"`
	LastError string `json:"lastError"`
}
