req, _ := http.NewRequest("DELETE", "https://db.fredyang.com/key/hello", nil)
req.Header.Set("X-Api-Key", apiKey)
http.DefaultClient.Do(req)
