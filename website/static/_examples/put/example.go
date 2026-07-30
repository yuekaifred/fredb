req, _ := http.NewRequest("PUT", "https://db.fredyang.com/key/hello", strings.NewReader("world"))
req.Header.Set("X-Api-Key", apiKey)
http.DefaultClient.Do(req)
