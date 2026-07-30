req, _ := http.NewRequest("GET", "https://db.fredyang.com/range?start=a&end=z", nil)
req.Header.Set("X-Api-Key", apiKey)
resp, _ := http.DefaultClient.Do(req)
