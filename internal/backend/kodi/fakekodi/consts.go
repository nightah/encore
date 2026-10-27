package fakekodi

// TimeLayout is how Kodi formats dateadded and lastplayed.
const TimeLayout = "2006-01-02 15:04:05"

// JSON-RPC error codes Kodi returns. NotFound and Unavailable are new in Kodi 22 (API 13.1000); older versions report both as InvalidParams.
const (
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeNotFound       = -32098
	CodeUnavailable    = -32097
)

const (
	audioPlayerID = 0
	videoPlayerID = 1
)

// Seek step sizes in seconds, Kodi's defaults.
const (
	smallStep = 30
	bigStep   = 600
)
