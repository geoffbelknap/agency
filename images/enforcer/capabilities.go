package main

// detectRequiredCaps inspects an OpenAI-format request body and returns
// the capabilities the request requires from the target model.
func detectRequiredCaps(body map[string]interface{}) []string {
	var caps []string

	if tools, ok := body["tools"].([]interface{}); ok && len(tools) > 0 {
		caps = append(caps, "tools")
	}

	if stream, ok := body["stream"].(bool); ok && stream {
		caps = append(caps, "streaming")
	}

	if messages, ok := body["messages"].([]interface{}); ok {
		for _, msg := range messages {
			m, ok := msg.(map[string]interface{})
			if !ok {
				continue
			}
			content, ok := m["content"].([]interface{})
			if !ok {
				continue
			}
			for _, block := range content {
				b, ok := block.(map[string]interface{})
				if !ok {
					continue
				}
				if b["type"] == "image_url" || b["type"] == "image" {
					caps = append(caps, "vision")
					return caps
				}
			}
		}
	}

	return caps
}
