package collector

// CollectMediaTypes fetches all configured media types returning mediatypeid and name.
func CollectMediaTypes(apiUrl, token string, req ApiRequester) ([]map[string]interface{}, error) {
	params := map[string]interface{}{
		"output": []string{"mediatypeid", "name"},
	}
	return CollectRawList(apiUrl, token, "mediatype.get", params, req)
}
