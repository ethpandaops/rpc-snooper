package protocol

type WSMessage struct {
	RequestID  uint64      `json:"reqid,omitempty"`
	ResponseID uint64      `json:"rspid,omitempty"`
	ModuleID   uint64      `json:"modid,omitempty"`
	Method     string      `json:"method"`
	Data       interface{} `json:"data,omitempty"`
	Error      *string     `json:"error,omitempty"`
	Timestamp  int64       `json:"time"`
}

type RegisterModuleRequest struct {
	Type   string                 `json:"type"`
	Name   string                 `json:"name"`
	Config map[string]interface{} `json:"config"`
}

type RegisterModuleResponse struct {
	Success  bool   `json:"success"`
	ModuleID uint64 `json:"module_id,omitempty"`
	Message  string `json:"message,omitempty"`
}

type HookEvent struct {
	ModuleID    uint64      `json:"module_id"`
	HookType    string      `json:"hook_type"`
	RequestID   string      `json:"request_id"`
	Data        interface{} `json:"data"`
	ContentType string      `json:"content_type"`
}
type CounterEvent struct {
	ModuleID    uint64            `json:"module_id"`
	Count       int64             `json:"count"`
	RequestType string            `json:"request_type"`
	Filters     map[string]string `json:"filters,omitempty"`
}

type TracerEvent struct {
	ModuleID     uint64 `json:"module_id"`
	RequestID    string `json:"request_id"`
	Duration     int64  `json:"duration_ms"`
	RequestSize  int64  `json:"request_size"`
	ResponseSize int64  `json:"response_size"`
	StatusCode   int    `json:"status_code"`
}
