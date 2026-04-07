package api

import (
	"encoding/json"
	"net/http"
)

func mcpCallHandler(reg *MCPToolRegistry, d *mcpDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name      string                 `json:"name"`
			Arguments map[string]interface{} `json:"arguments"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"content": []map[string]string{{"type": "text", "text": "invalid JSON: " + err.Error()}},
				"isError": true,
			})
			return
		}

		// Forward env vars from X-Agency-Env header
		if envHeader := r.Header.Get("X-Agency-Env"); envHeader != "" {
			if req.Arguments == nil {
				req.Arguments = make(map[string]interface{})
			}
			req.Arguments["_env"] = envHeader
		}

		text, isErr := reg.Call(req.Name, d, req.Arguments)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"content": []map[string]string{{"type": "text", "text": text}},
			"isError": isErr,
		})
	}
}
