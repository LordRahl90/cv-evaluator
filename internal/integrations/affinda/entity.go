package affinda

type DocumentResponse struct {
	Extractor string       `json:"extractor"`
	Meta      DocumentMeta `json:"meta"`
	Error     Error        `json:"error"`
	Warnings  Warnings     `json:"warnings"`
}

type DocumentMeta struct {
	ID         string `json:"identifier"`
	CustomerID string `json:"customIdentifier"`
	Ready      bool   `json:"ready"`
	Failed     bool   `json:"failed"`
}

type Error struct {
	Code   string `json:"errorCode"`
	Detail string `json:"errorDetail"`
}

type Warnings struct {
	Code   string `json:"warningCode"`
	Detail string `json:"warningDetail"`
}
