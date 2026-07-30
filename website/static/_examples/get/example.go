req, _ := http.NewRequest("GET", "https://db.fredyang.com/key/hello", nil)
req.Header.Set("X-Api-Key", apiKey)
resp, _ := http.DefaultClient.Do(req)
