package integration

import "encoding/json"

var (
	jsonMarshal   = json.Marshal
	jsonUnmarshal = func(s string, v any) error { return json.Unmarshal([]byte(s), v) }
)
