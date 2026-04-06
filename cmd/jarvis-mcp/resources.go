package main

var allResources = []ResourceDefinition{
	{
		URI:         "jarvis://sessions",
		Name:        "Active Sessions",
		Description: "List of all active Jarvis sessions with their status, working directory, and metadata",
		MimeType:    "application/json",
	},
	{
		URI:         "jarvis://agents",
		Name:        "Active Agents",
		Description: "List of all active sub-agents with their status, parent session, and progress",
		MimeType:    "application/json",
	},
}

func handleResourceRead(uri string) ([]ResourceContent, error) {
	switch uri {
	case "jarvis://sessions":
		data, err := apiGet("/api/sessions")
		if err != nil {
			return nil, err
		}
		result := toolSuccess(data)
		return []ResourceContent{
			{
				URI:      uri,
				MimeType: "application/json",
				Text:     result.Content[0].Text,
			},
		}, nil

	case "jarvis://agents":
		data, err := apiGet("/api/subagents")
		if err != nil {
			return nil, err
		}
		result := toolSuccess(data)
		return []ResourceContent{
			{
				URI:      uri,
				MimeType: "application/json",
				Text:     result.Content[0].Text,
			},
		}, nil

	default:
		return nil, nil
	}
}
