package hub

type FlowStepResponseDef struct {
	Type  string       `json:"type"`
	Value BaseValueDef `json:"value,omitempty"`
}

type ApiDef struct {
	Name             string          `json:"name"`
	Command          string          `json:"command"`
	Private          string          `json:"private"`
	Description      string          `json:"description"`
	ResultKey        string          `json:"resultKey"`
	Args             *[]BaseParamDef `json:"args"`
	OriginParameters *[]BaseParamDef `json:"origin"`
	DefaultRight     string          `json:"defaultRight"`
	Type             string          `json:"type"`
}
